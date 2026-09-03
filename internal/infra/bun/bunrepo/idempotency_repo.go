package bunrepo

import (
	"context"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/pkg/idgen"
)

// IdempotencyStore 是 databases.IdempotencyStore 的 public 控制面实现
// （bun + 原生 SQL）：TryClaim 的原子性由主键唯一约束仲裁——并发 INSERT
// ON CONFLICT DO NOTHING 在唯一索引上串行等待，同一时刻至多一个调用方拿到
// Acquired。
type IdempotencyStore struct {
	db *clients.Database
}

func NewIdempotencyStore(db *clients.Database) *IdempotencyStore {
	return &IdempotencyStore{db: db}
}

var _ databases.IdempotencyStore = (*IdempotencyStore)(nil)

// 幂等行状态。
const (
	idempotencyStateInFlight = "in_flight"
	idempotencyStateDone     = "done"
)

func (s *IdempotencyStore) TryClaim(ctx context.Context, key databases.IdempotencyKey, fingerprint string) (databases.IdempotencyClaim, error) {
	conn := s.db.Conn(ctx)
	// 惰性清理：写入时顺带删除过期行（含崩溃残留的 in_flight）。清理是
	// best-effort——失败不阻断认领主路径，过期行也会在下方的重认领分支兜底。
	_, _ = conn.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE expires_at < now()`)

	token := idgen.UUID().String()
	res, err := conn.ExecContext(ctx,
		`INSERT INTO idempotency_keys
		   (project_id, actor_id, request_id, fingerprint, claim_token, state, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, now() + (? * interval '1 second'))
		 ON CONFLICT (project_id, actor_id, request_id) DO NOTHING`,
		key.ProjectID, key.ActorID, key.RequestID, fingerprint, token,
		idempotencyStateInFlight, databases.IdempotencyInFlightTTL.Seconds(),
	)
	if err != nil {
		return databases.IdempotencyClaim{}, err
	}
	if inserted, _ := res.RowsAffected(); inserted > 0 {
		return databases.IdempotencyClaim{State: databases.IdempotencyClaimAcquired, Token: token}, nil
	}

	var rowFP, rowState string
	var rowPayload []byte
	err = conn.QueryRowContext(ctx,
		`SELECT fingerprint, state, response_payload FROM idempotency_keys
		 WHERE project_id = ? AND actor_id = ? AND request_id = ?`,
		key.ProjectID, key.ActorID, key.RequestID,
	).Scan(&rowFP, &rowState, &rowPayload)
	if err != nil {
		return databases.IdempotencyClaim{}, err
	}
	if rowFP != fingerprint {
		return databases.IdempotencyClaim{}, databases.ErrIdempotencyKeyConflict
	}
	if rowState == idempotencyStateDone {
		return databases.IdempotencyClaim{State: databases.IdempotencyClaimDone, Payload: rowPayload}, nil
	}
	return databases.IdempotencyClaim{State: databases.IdempotencyClaimInFlight}, nil
}

func (s *IdempotencyStore) Complete(ctx context.Context, key databases.IdempotencyKey, token string, payload []byte) error {
	// payload 以 string 绑定并显式 ::jsonb：pgdriver 对 []byte 走 bytea 编码，
	// PG 无法按 jsonb 输入语法解析（22P02）。
	_, err := s.db.Conn(ctx).ExecContext(ctx,
		`UPDATE idempotency_keys
		 SET state = ?, response_payload = ?::jsonb, expires_at = now() + (? * interval '1 second'), updated_at = now()
		 WHERE project_id = ? AND actor_id = ? AND request_id = ? AND claim_token = ? AND state = ?`,
		idempotencyStateDone, string(payload), databases.IdempotencyDoneTTL.Seconds(),
		key.ProjectID, key.ActorID, key.RequestID, token, idempotencyStateInFlight,
	)
	// 0 行（token 因过期被重认领）不是错误：写已成功，只损失本条缓存。
	return err
}

func (s *IdempotencyStore) Release(ctx context.Context, key databases.IdempotencyKey, token string) error {
	_, err := s.db.Conn(ctx).ExecContext(ctx,
		`DELETE FROM idempotency_keys
		 WHERE project_id = ? AND actor_id = ? AND request_id = ? AND claim_token = ? AND state = ?`,
		key.ProjectID, key.ActorID, key.RequestID, token, idempotencyStateInFlight,
	)
	return err
}

// ExpireAt 供集成测试直接操纵 TTL（测试辅助，不进端口契约）。
func (s *IdempotencyStore) ExpireAt(ctx context.Context, key databases.IdempotencyKey, at time.Time) error {
	_, err := s.db.Conn(ctx).ExecContext(ctx,
		`UPDATE idempotency_keys SET expires_at = ? WHERE project_id = ? AND actor_id = ? AND request_id = ?`,
		at, key.ProjectID, key.ActorID, key.RequestID,
	)
	return err
}
