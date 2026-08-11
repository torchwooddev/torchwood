package projects

import "time"

type Project struct {
	ID          string
	Name        string
	Description string
	Status      string
	Settings    map[string]any
	InternalID  int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type APIKey struct {
	ID         string
	ProjectID  string
	Name       string
	SecretHash string
	Scopes     []string
	ExpireAt   *time.Time
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Admin struct {
	ID           string
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
