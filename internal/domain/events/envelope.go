// Package events 定义用户集合文档写事件的信封与服务端 ACL 快照
// （v2 设计 §2）。
package events

import (
	"fmt"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
)

const (
	// EventDocumentsCreate 由 Create 与 Upsert 插入支产生（写后全文档）。
	EventDocumentsCreate = "databases.documents.create"
	// EventDocumentsUpdate 由 Update / Increment / Upsert 更新支与 BulkUpdate
	// 每篇产生（写后全文档，acl=写前）。
	EventDocumentsUpdate = "databases.documents.update"
	// EventDocumentsDelete 由 Delete / BulkDelete 每篇产生（无 data，
	// version=删除前，acl=写前）。
	EventDocumentsDelete = "databases.documents.delete"
)

// ACLSnapshot 是事件投递过滤用的服务端 ACL 快照：create 用写后
// _perms + 当时 collection ACL；update/delete 用写前。集合 ACL 事后变更
// 不影响已发出事件的可见性。快照只进 outbox.payload，绝不下发出站 WS。
type ACLSnapshot struct {
	DocumentSecurity      bool
	CollectionPermissions []databases.Permission
	DocumentPermissions   []databases.Permission
	DocHasPerms           bool
}

// Envelope 是文档写事件的信封（outbox 内存完整；WS 出站必须走
// ClientPayload 剥掉 acl）。
type Envelope struct {
	EventID      string
	Event        string
	ProjectID    string
	DatabaseID   string
	CollectionID string
	DocumentID   string
	Version      int64
	CreatedAt    time.Time
	Truncated    bool
	Data         *databases.Document // delete 时 nil
	ACL          ACLSnapshot

	// v3 经济事件扩展（设计 §5.1）：Domain 非空表示非文档事件
	// （payments / economy / subscriptions），Channel 显式给出扇出频道
	// （D17 单一 accounts.{userId}），Attrs 携带事件专属字段且不含隐私。
	// 文档事件三个字段均为零值，序列化与扇出行为与 v2 完全一致。
	Domain  string
	Channel string
	Attrs   map[string]any
}

// IsEconomy 报告是否为 v3 经济事件（显式 domain/channel 的非文档事件）。
func (e Envelope) IsEconomy() bool { return e.Domain != "" }

// CollectionChannel 返回集合频道名（topic 与订阅用）。
func (e Envelope) CollectionChannel() string {
	return fmt.Sprintf("databases.%s.collections.%s", e.DatabaseID, e.CollectionID)
}

// DocumentChannel 返回文档频道名（Hub 按信封 fan-out 两路）。
func (e Envelope) DocumentChannel() string {
	return fmt.Sprintf("databases.%s.collections.%s.documents.%s", e.DatabaseID, e.CollectionID, e.DocumentID)
}

// DocumentPayload 把领域文档映射为与 REST Document 同形的 payload：
// 含顶层 id / version，不含任何 _ 系统列。
func DocumentPayload(d *databases.Document) map[string]any {
	perms := make([]string, 0, len(d.Permissions))
	for _, p := range d.Permissions {
		perms = append(perms, databases.FormatPermissionString(p))
	}
	return map[string]any{
		"id":          d.ID,
		"data":        d.Data,
		"permissions": perms,
		"created_at":  d.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":  d.UpdatedAt.UTC().Format(time.RFC3339),
		"version":     d.Version,
	}
}

// ClientPayload 返回出站 JSON（WS 帧 / 客户端可见）：**不含 acl**。
// 保留 event_id / event / 资源 id / version / created_at / truncated / data；
// delete 事件无 data 键。
// 经济事件（Domain 非空）：文档专属字段不出现，改为 domain + channel
// + Attrs（channel 必须进 payload——worker → Stream → Hub 链路只认 payload）。
func (e Envelope) ClientPayload() map[string]any {
	if e.IsEconomy() {
		m := map[string]any{
			"event_id":   e.EventID,
			"event":      e.Event,
			"project_id": e.ProjectID,
			"domain":     e.Domain,
			"channel":    e.Channel,
			"created_at": e.CreatedAt.UTC().Format(time.RFC3339),
		}
		for k, v := range e.Attrs {
			m[k] = v
		}
		return m
	}
	m := map[string]any{
		"event_id":      e.EventID,
		"event":         e.Event,
		"project_id":    e.ProjectID,
		"database_id":   e.DatabaseID,
		"collection_id": e.CollectionID,
		"document_id":   e.DocumentID,
		"version":       e.Version,
		"created_at":    e.CreatedAt.UTC().Format(time.RFC3339),
		"truncated":     e.Truncated,
	}
	if e.Data != nil {
		m["data"] = DocumentPayload(e.Data)
	}
	return m
}

// VisibleTo 按快照 ACL 判断 principal 是否可见该事件：系统主体旁路，
// 其余复用 AllowsDocumentAccess(..., "read", roles) 的文档读语义。
// 用户集合事件快照不经集合级 read 预检（产品：订阅集合频道不要求
// collection read，每条事件再按 _perms 过滤）。
func VisibleTo(acl ACLSnapshot, p databases.Principal) bool {
	if p.BypassesDocumentACL() {
		return true
	}
	coll := &databases.Collection{
		DocumentSecurity: acl.DocumentSecurity,
		Permissions:      acl.CollectionPermissions,
		IsSystem:         false, // 用户集合事件
	}
	return databases.AllowsDocumentAccess(coll, acl.DocumentPermissions, acl.DocHasPerms, "read", p.Roles)
}
