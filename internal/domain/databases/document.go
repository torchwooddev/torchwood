package databases

import "time"

type Attribute struct {
	ID       string
	Key      string
	Type     string // string, integer, float, boolean, datetime, email, url, json
	Size     int
	Required bool
	Default  any
	Array    bool
	Options  map[string]any
}

type Index struct {
	ID         string
	Type       string // key, unique, fulltext
	Attributes []string
	Orders     []string
}

type Permission struct {
	Type string // read, create, update, delete
	Role string // any, users, user:{id}, keys, admin, team:{id}, ...
}

type Document struct {
	ID          string
	Tenant      int64
	Data        map[string]any
	Permissions []Permission
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   string
	UpdatedBy   string
}

type Query struct {
	Queries   []string
	PageSize  int32
	PageToken string
}

// ListQuery carries pagination parameters for collection listing.
type ListQuery struct {
	PageSize  int32
	PageToken string
}

// ListMeta reports pagination metadata for collection listing.
type ListMeta struct {
	TotalCount    int64
	NextPageToken string
}

type CollectionPatch struct {
	Name             string
	Permissions      *[]Permission
	DocumentSecurity *bool
	Disabled         *bool
}

type DocumentUpdate struct {
	Document    Document
	Permissions []Permission
	Increment   map[string]int64
}

type DocumentList struct {
	Documents     []Document
	TotalCount    int64
	NextPageToken string
}

type Collection struct {
	ID               string
	DatabaseID       string
	ProjectID        string
	Name             string
	DocumentSecurity bool
	Disabled         bool
	IsSystem         bool
	Permissions      []Permission
	Attributes       []Attribute
	Indexes          []Index
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
