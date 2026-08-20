package model

import "github.com/uptrace/bun"

// ProviderResourceIndex 是 public.provider_resource_index。
type ProviderResourceIndex struct {
	bun.BaseModel `bun:"table:provider_resource_index,alias:pri"`

	Provider    string `bun:"provider,pk"`
	Kind        string `bun:"kind,pk"`
	ProviderRef string `bun:"provider_ref,pk"`
	ProjectID   string `bun:"project_id,notnull"`
}
