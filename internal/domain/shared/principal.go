package shared

import "github.com/torchwooddev/torchwood/pkg/idgen"

type ActorKind string

const (
	ActorKindEndUser ActorKind = "end_user"
	ActorKindAdmin   ActorKind = "admin"
	ActorKindService ActorKind = "service"
)

func (k ActorKind) IsValid() bool {
	switch k {
	case ActorKindEndUser, ActorKindAdmin, ActorKindService:
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

type Principal struct {
	ActorID         idgen.ID
	ActorKind       ActorKind
	CredentialType  CredentialType
	IsPlatformAdmin bool
	ProjectID       string
	UserID          string
	APIKeyID        string
	SessionID       string
	Roles           []string
	Permissions     []string // scopes for API keys
	Email           string
}

func (p *Principal) IsAuthenticated() bool {
	return p != nil && (p.UserID != "" || p.APIKeyID != "")
}

func (p *Principal) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func (p *Principal) HasScope(scope string) bool {
	for _, s := range p.Permissions {
		if s == scope {
			return true
		}
	}
	return false
}

func (p *Principal) HasPermission(perm string) bool {
	return p.HasRole(perm) || p.HasScope(perm)
}

// HasAnyPermission 报告主体现有角色/scope 是否命中任一权限。
// 空列表恒返回 true（fail-open），该行为依赖调用点的守门约束：
// 所有 ACCESS_PERMISSION 方法都在 server 启动期（collectMethodsByAccess）
// 强制要求显式非空 permissions，未经守门的直接调用需自行判空把关。
func (p *Principal) HasAnyPermission(perms []string) bool {
	if len(perms) == 0 {
		return true
	}
	for _, perm := range perms {
		if p.HasPermission(perm) {
			return true
		}
	}
	return false
}
