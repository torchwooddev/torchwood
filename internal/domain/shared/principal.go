package shared

import (
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/pkg/idgen"
)

type ActorKind string

const (
	ActorKindEndUser ActorKind = "end_user"
	ActorKindAdmin   ActorKind = "admin"
	ActorKindService ActorKind = "service" // 项目 API key
	ActorKindSystem  ActorKind = "system"
)

func (k ActorKind) IsValid() bool {
	switch k {
	case ActorKindEndUser, ActorKindAdmin, ActorKindService, ActorKindSystem:
		return true
	}
	return false
}

type CredentialType string

const (
	CredentialTypeToken   CredentialType = "token"
	CredentialTypeSession CredentialType = "session"
	CredentialTypeAPIKey  CredentialType = "api_key"
)

const (
	ConsoleSessionCookieName = "TORCHWOOD_session_console"
	SessionCookiePrefix      = "TORCHWOOD_session_"
)

type Principal struct {
	ActorID         idgen.ID
	ActorKind       ActorKind
	CredentialType  CredentialType
	IsPlatformAdmin bool
	ProjectID       string
	UserID          string // 仅 EndUser
	AdminID         string // 仅 Admin；禁止把 admin id 塞进 UserID
	APIKeyID        string // 仅 Service
	SessionID       string
	Roles           []string // 文档 ACL / console RBAC，不是 API scope
	Permissions     []string // API key scopes
	Email           string
}

// NewSystemPrincipal 构造内部 System 主体（worker / 履约），不是缺字段的 API key。
func NewSystemPrincipal(projectID string) *Principal {
	return &Principal{
		ActorID:   idgen.ID("system"),
		ActorKind: ActorKindSystem,
		ProjectID: projectID,
	}
}

func (p *Principal) IsSystem() bool {
	return p != nil && p.ActorKind == ActorKindSystem
}

func (p *Principal) IsAuthenticated() bool {
	if p == nil {
		return false
	}
	switch p.ActorKind {
	case ActorKindEndUser:
		return p.UserID != ""
	case ActorKindAdmin:
		return p.AdminID != "" || p.ActorID != ""
	case ActorKindService:
		return p.APIKeyID != ""
	case ActorKindSystem:
		return true
	default:
		return false
	}
}

func (p *Principal) HasRole(role string) bool {
	if p == nil {
		return false
	}
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func (p *Principal) HasScope(scope string) bool {
	if p == nil {
		return false
	}
	for _, s := range p.Permissions {
		if s == scope {
			return true
		}
	}
	return false
}

// HasAnyRole 报告是否命中任一角色。空列表 fail-closed（false）；
// proto ACCESS_PERMISSION 的空列表由启动期 collectMethodsByAccess 守门。
func (p *Principal) HasAnyRole(roles []string) bool {
	if p == nil || len(roles) == 0 {
		return false
	}
	for _, role := range roles {
		if p.HasRole(role) {
			return true
		}
	}
	return false
}

// DocPrincipal 投影到文档 ACL 视图。System 走 databases.SystemPrincipal。
func (p *Principal) DocPrincipal() databases.Principal {
	if p == nil {
		return databases.GuestPrincipal
	}
	if p.IsSystem() {
		return databases.SystemPrincipal
	}
	return databases.Principal{Roles: p.Roles, PlatformAdmin: p.IsPlatformAdmin}
}
