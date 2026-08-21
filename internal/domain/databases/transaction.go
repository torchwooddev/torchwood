package databases

import (
	"context"
	"errors"
	"time"
)

// 事务状态（v2 设计 §5.1）。
const (
	TransactionStatusPending    = "pending"
	TransactionStatusCommitted  = "committed"
	TransactionStatusRolledBack = "rolled_back"
	TransactionStatusExpired    = "expired"
)

// 事务操作类型。
const (
	TransactionOpCreate = "create"
	TransactionOpUpdate = "update"
	TransactionOpDelete = "delete"
	TransactionOpUpsert = "upsert"
)

// 事务稳定错误消息（v2 设计 §稳定错误消息）：均映射 FailedPrecondition。
var (
	// ErrTransactionAlreadyPending 是同一 created_by+project+database 已有
	// pending 事务时再 Create 命中部分唯一索引（23505）返回的错误。
	ErrTransactionAlreadyPending = errors.New("transaction_already_pending")
	// ErrTransactionExpired 是 Commit/追加时事务已过 TTL 返回的错误
	// （行锁内就地 SET expired 并 COMMIT）。
	ErrTransactionExpired = errors.New("transaction_expired")
	// ErrTransactionNotPending 是对非 pending（committed/rolled_back/expired）
	// 事务再 Commit/追加/Rollback 返回的错误。
	ErrTransactionNotPending = errors.New("transaction_not_pending")
	// ErrSystemCollectionNotAllowed 是事务操作触碰系统集合（或 is_system
	// 集合）时返回的错误；追加阶段校验失败不改 status。
	ErrSystemCollectionNotAllowed = errors.New("system_collection_not_allowed")
	// ErrTransactionOpsLimit 是追加第 101 条 op 时返回的错误。
	ErrTransactionOpsLimit = errors.New("transaction_ops_limit")
)

// Transaction 是单库事务的领域模型。
type Transaction struct {
	ID         string
	ProjectID  string
	DatabaseID string
	Status     string
	// CreatedBy 为创建者标识：user:{UserID} / key:{APIKeyID} / admin:{ActorID}。
	CreatedBy string
	ExpireAt  time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TransactionOp 是事务内暂存的一条文档写操作；permissions 为空（nil）
// 表示「不改动文档权限」（与单条 UpdateDocument 语义一致）。
type TransactionOp struct {
	ID              string
	TransactionID   string
	Seq             int32
	OpType          string
	CollectionID    string
	DocumentID      string
	Data            map[string]any
	Permissions     []Permission
	Increment       map[string]int64
	Version         *int64
	ConflictColumns []string
	CreatedAt       time.Time
}

// TransactionRepository 是事务元数据仓储端口。方法加入调用方的 uow.Run；
// 实现可从 ctx 读取连接（锁行、追加、置状态与文档写同 COMMIT）。
type TransactionRepository interface {
	Create(ctx context.Context, tx Transaction) error
	// Get 未命中返回 (nil, nil)。
	Get(ctx context.Context, projectID, databaseID, txID string) (*Transaction, error)
	// LockPending 以 FOR UPDATE 锁行（必须在事务内调用）；未命中返回 (nil, nil)。
	LockPending(ctx context.Context, projectID, databaseID, txID string) (*Transaction, error)
	AppendOp(ctx context.Context, op TransactionOp) error
	ListOps(ctx context.Context, txID string) ([]TransactionOp, error)
	SetStatus(ctx context.Context, txID, status string) error
}
