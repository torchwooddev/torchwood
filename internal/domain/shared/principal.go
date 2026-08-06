package shared

import "github.com/torchwoodio/torchwood/pkg/idgen"

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
