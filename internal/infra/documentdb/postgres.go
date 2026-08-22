package documentdb

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"

	"github.com/uptrace/bun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

// ErrDuplicateKey re-exports the domain duplicate-key error.
var ErrDuplicateKey = databases.ErrDuplicateKey

// ErrPermissionDenied re-exports the domain permission error.
var ErrPermissionDenied = databases.ErrPermissionDenied

var safeNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
var docIDRe = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,64}$`)

// systemCollectionsWriteProtected 是禁止非系统主体直接写入的系统集合（纵深防御，
// 正常情况下客户端 API 已在用例层拦截）。仅对项目数据面 sentinel 生效，
// 业务库（含 default）中的同名集合不受影响。
var systemCollectionsWriteProtected = map[string]struct{}{
	"users":      {},
	"sessions":   {},
	"identities": {},
}

func isWriteProtectedSystemCollection(databaseID, collectionID string) bool {
	if databaseID != ident.ProjectDataPlaneID {
		return false
	}
	_, ok := systemCollectionsWriteProtected[collectionID]
	return ok
}

const maxQueryLimit = 100

// maxQueryOffset 是 offset 深翻页上限，超过则拒绝（防 10^9 量级 offset 拖慢查询）。
const maxQueryOffset = 10000

// A2 输入上限：queries 条数 / 单条查询串长度 / equal 多值个数。
const maxQueryCount = 100
const maxQueryStringLen = 4096
const maxFilterValues = 1000

type postgresDocumentDB struct {
	db  *clients.Database
	pub shared.EventPublisher // nil 视为 nop（单测）；写路径同事务写入 outbox

	// in-process caches keyed by projectID; safe for concurrent use.
	internalIDCache sync.Map // projectID -> int64
	// versionColumns 记录已确认 _version 为 bigint 的 "schema.collection" 键，
	// 避免每次写/读重复查 information_schema；**只缓存已提交的列**。
	versionColumns sync.Map // "schema.collection" -> struct{}
	// versionAlterTx 记录"本事务内新建 _version 列"的键（txid|schema.collection）。
	// CREATE TABLE 带列或 ALTER ADD 都会打标，避免 versionColumnReady 把未提交列写入 cache。
	// DDL / catalog reconcile 才写此标记，不在文档写热路径。
	versionAlterTx sync.Map // "txid|schema.collection" -> struct{}
}

func NewPostgresDocumentDB(db *clients.Database, pub shared.EventPublisher) databases.DocumentDB {
	return &postgresDocumentDB{db: db, pub: pub}
}

var _ databases.DocumentDB = (*postgresDocumentDB)(nil)

func (p *postgresDocumentDB) conn(ctx context.Context) bun.IDB {
	return p.db.Conn(ctx)
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func mapIdentError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ident.ErrInvalidSchemaResourceID) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return err
}

func pgTextArray(items []string) string {
	var parts []string
	for _, item := range items {
		parts = append(parts, `"`+strings.ReplaceAll(item, `"`, `""`)+`"`)
	}
	return `{` + strings.Join(parts, ",") + `}`
}
