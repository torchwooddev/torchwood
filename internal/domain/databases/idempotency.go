package databases

import (
	"context"
	"errors"
	"time"
)

// 写幂等端口（redesign §4.1/§10.1）：写操作携带 request_id，24h 内同键重放
// 返回首次成功响应；只缓存成功（失败释放、重试重新执行）。键作用域 =
// (project_id, actor_id, request_id)——不同 actor 同 key 不冲突。

// IdempotencyDoneTTL 是已完成记录的保留窗口（重放有效期）；从完成时刻起算。
const IdempotencyDoneTTL = 24 * time.Hour

// IdempotencyInFlightTTL 是 in_flight 占位行的兜底过期：执行中的写未完成落
// 此状态，崩溃残留行到期后可被重新认领（期间同键请求收到 IN_PROGRESS）。
// 必须显著大于最慢合法写请求的执行时长。
const IdempotencyInFlightTTL = 5 * time.Minute

// IdempotencyKey 唯一确定一条幂等记录。
type IdempotencyKey struct {
	ProjectID string
	ActorID   string
	RequestID string
}

// IdempotencyClaimState 是 TryClaim 的三态结果。
type IdempotencyClaimState int

const (
	// IdempotencyClaimAcquired：本请求获得执行权（已插入 in_flight 占位行，
	// 携带 claim token；完成时 Complete，失败时 Release）。
	IdempotencyClaimAcquired IdempotencyClaimState = iota
	// IdempotencyClaimDone：存在同指纹的成功记录，Payload 为缓存响应。
	IdempotencyClaimDone
	// IdempotencyClaimInFlight：同指纹请求执行中，调用方可短轮询等待。
	IdempotencyClaimInFlight
)

// IdempotencyClaim 是 TryClaim 的结果。
type IdempotencyClaim struct {
	State   IdempotencyClaimState
	Token   string // State==Acquired 时的执行权凭证（Complete/Release 回传）
	Payload []byte // State==Done 时的缓存响应 JSON
}

// ErrIdempotencyKeyConflict：同 key 携带了不同请求指纹（客户端复用 key 发了
// 不同请求）。映射 InvalidArgument / IDEMPOTENCY.KEY_CONFLICT。
var ErrIdempotencyKeyConflict = errors.New("idempotency key conflict")

// IdempotencyStore 是幂等记录存储端口。实现必须保证 TryClaim 对同键并发调用
// 的原子性（唯一键仲裁）：同一时刻至多一个调用方拿到 Acquired。
type IdempotencyStore interface {
	// TryClaim 原子认领幂等键（顺带惰性清理过期行）：
	//   - 无行/已过期 → 插入 in_flight 行，返回 Acquired + 新 token；
	//   - 同指纹 done 行 → 返回 Done + 缓存 payload；
	//   - 同指纹 in_flight 行 → 返回 InFlight；
	//   - 指纹不同（无论状态）→ ErrIdempotencyKeyConflict。
	TryClaim(ctx context.Context, key IdempotencyKey, fingerprint string) (IdempotencyClaim, error)
	// Complete 将本 token 认领的 in_flight 行落为 done 并写入响应载荷
	//（TTL 从完成时刻起算）。写已成功，本调用失败只损失缓存不损失写入，
	// 调用方按 best-effort 处理；token 失效（过期被重认领）不视为错误。
	Complete(ctx context.Context, key IdempotencyKey, token string, payload []byte) error
	// Release 删除本 token 认领的 in_flight 行（执行失败不缓存，重试重新执行）。
	Release(ctx context.Context, key IdempotencyKey, token string) error
}
