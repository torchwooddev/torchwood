package model

import (
	"time"

	"github.com/uptrace/bun"
)

type ProjectOAuthProvider struct {
	bun.BaseModel `bun:"table:project_oauth_providers,alias:pop"`

	ProjectID    string    `bun:"project_id,pk"`
	Provider     string    `bun:"provider,pk"`
	Enabled      bool      `bun:"enabled,notnull"`
	ClientID     string    `bun:"client_id,notnull"`
	ClientSecret string    `bun:"client_secret,notnull"`
	Scopes       []string  `bun:"scopes,array,notnull,default:'{}'"`
	CreatedAt    time.Time `bun:"created_at,notnull,default:current_timestamp"`
	UpdatedAt    time.Time `bun:"updated_at,notnull,default:current_timestamp"`
}
