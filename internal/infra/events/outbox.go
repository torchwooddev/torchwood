// Package events 提供用户集合文档写事件的 transactional outbox 适配器
// （v2 设计 §3）：把信封（含服务端 acl 快照）INSERT 进
// public.document_events_outbox，与文档写同一段事务。
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/pkg/idgen"
)

// maxEnvelopeBytes 是信封 JSON 的载荷上限（256 KiB）。超出时截断事件、
// 不回滚业务写（v2 设计 §2.3）。
const maxEnvelopeBytes = 256 * 1024

type eventOutbox struct {
	db *clients.Database
}

// NewEventOutbox 构造 EventPublisher 的 outbox 实现。调用方应在 uow.Run
// 内 Publish，与业务写同一工作单元；实现仍从 ctx 读取连接（Conn(ctx)）：
// 已在工作单元内则复用外层事务（与文档行同 COMMIT），未在工作单元内则
// 自行短事务插入。
func NewEventOutbox(db *clients.Database) *eventOutbox {
	return &eventOutbox{db: db}
}

func (o *eventOutbox) Publish(ctx context.Context, ev domainevents.Envelope) error {
	if ev.EventID == "" {
		ev.EventID = idgen.ULID().String()
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now()
	}
	if txID := domainevents.TransactionIDFrom(ctx); txID != "" {
		ev.TransactionID = txID
	}
	payload, err := marshalEnvelope(ev)
	if err != nil {
		return err
	}
	insert := func(ctx context.Context) error {
		topic := ev.CollectionChannel()
		var channel *string
		if ev.IsEconomy() {
			// 经济事件：topic 落事件名，channel 落显式扇出频道（D17）。
			topic = ev.Event
			ch := ev.Channel
			channel = &ch
		}
		_, err := o.db.Conn(ctx).NewInsert().Model(&model.DocumentEventsOutbox{
			EventID:     ev.EventID,
			ProjectID:   ev.ProjectID,
			Topic:       topic,
			Channel:     channel,
			Payload:     payload,
			CreatedAt:   ev.CreatedAt,
			AvailableAt: time.Now(),
		}).Exec(ctx)
		return err
	}
	if clients.InTx(ctx) {
		return insert(ctx)
	}
	return o.db.RunInTx(ctx, func(txCtx context.Context) error {
		return insert(txCtx)
	})
}

// marshalEnvelope 序列化完整信封并按 256 KiB 上限截断：
// 1) 整体超限 → 去掉 data、标记 truncated（业务写不回滚）；
// 2) acl 仍超限（极端权限列表）→ 逐条截断 acl 数组并记日志。
// 经济事件（Domain 非空）载荷小且无 acl / data，直接序列化。
func marshalEnvelope(ev domainevents.Envelope) (json.RawMessage, error) {
	if ev.IsEconomy() {
		return json.Marshal(envelopePayloadMap(ev))
	}
	payload, err := json.Marshal(envelopePayloadMap(ev))
	if err != nil {
		return nil, err
	}
	if len(payload) <= maxEnvelopeBytes {
		return payload, nil
	}
	ev.Truncated = true
	ev.Data = nil
	payload, err = json.Marshal(envelopePayloadMap(ev))
	if err != nil {
		return nil, err
	}
	for len(payload) > maxEnvelopeBytes && (len(ev.ACL.CollectionPermissions) > 0 || len(ev.ACL.DocumentPermissions) > 0) {
		if len(ev.ACL.CollectionPermissions) > 0 {
			ev.ACL.CollectionPermissions = ev.ACL.CollectionPermissions[:len(ev.ACL.CollectionPermissions)-1]
		}
		if len(ev.ACL.DocumentPermissions) > 0 {
			ev.ACL.DocumentPermissions = ev.ACL.DocumentPermissions[:len(ev.ACL.DocumentPermissions)-1]
		}
		payload, err = json.Marshal(envelopePayloadMap(ev))
		if err != nil {
			return nil, err
		}
	}
	if len(payload) > maxEnvelopeBytes {
		slog.Warn("outbox envelope exceeds size limit after acl truncation",
			"event_id", ev.EventID, "event", ev.Event, "bytes", len(payload))
	}
	return payload, nil
}

// envelopePayloadMap 组装 outbox.payload：ClientPayload()（无 acl）之上
// 追加服务端投递过滤用的 acl 快照（经济事件无 acl，设计 §5.1）。
// 与 outbox.payload 同形的完整信封也是日后 Redis Stream 条目的载荷
// （含 acl，Hub 侧再剥）。
func envelopePayloadMap(ev domainevents.Envelope) map[string]any {
	m := ev.ClientPayload()
	if ev.IsEconomy() {
		return m
	}
	m["acl"] = map[string]any{
		"document_security":      ev.ACL.DocumentSecurity,
		"collection_permissions": permissionStrings(ev.ACL.CollectionPermissions),
		"document_permissions":   permissionStrings(ev.ACL.DocumentPermissions),
		"doc_has_perms":          ev.ACL.DocHasPerms,
	}
	return m
}

func permissionStrings(perms []databases.Permission) []string {
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		out = append(out, databases.FormatPermissionString(p))
	}
	return out
}

var _ shared.EventPublisher = (*eventOutbox)(nil)
