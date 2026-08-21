package users

import (
	"context"
	"errors"
	"strings"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainusers "github.com/torchwooddev/torchwood/internal/domain/users"
	"github.com/torchwooddev/torchwood/pkg/query"
)

// DocumentRepository 把 User 聚合落到项目数据面系统集合 users（sentinel `_`）。
type DocumentRepository struct {
	docDB databases.DocumentDB
}

func NewDocumentRepository(docDB databases.DocumentDB) *DocumentRepository {
	return &DocumentRepository{docDB: docDB}
}

var _ domainusers.Repository = (*DocumentRepository)(nil)

func (r *DocumentRepository) GetByEmail(ctx context.Context, projectID, email string) (*domainusers.User, error) {
	return r.getByAttr(ctx, projectID, "email", domainusers.NormalizeEmail(email))
}

func (r *DocumentRepository) GetByPhone(ctx context.Context, projectID, phone string) (*domainusers.User, error) {
	return r.getByAttr(ctx, projectID, "phone", strings.TrimSpace(phone))
}

func (r *DocumentRepository) GetByID(ctx context.Context, projectID, id string) (*domainusers.User, error) {
	if r == nil || r.docDB == nil || strings.TrimSpace(id) == "" {
		return nil, nil
	}
	doc, err := r.docDB.GetDocument(ctx, projectID, databases.SystemDatabaseID, domainusers.CollectionID, id, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	return userFromDocument(doc), nil
}

func (r *DocumentRepository) Insert(ctx context.Context, projectID string, user *domainusers.User) error {
	if r == nil || r.docDB == nil {
		return domainusers.ErrUserIDRequired
	}
	if user == nil || strings.TrimSpace(user.ID) == "" {
		return domainusers.ErrUserIDRequired
	}
	doc := databases.Document{ID: user.ID, Data: user.DocumentData()}
	_, err := r.docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, domainusers.CollectionID, doc, documentPermissions(user.ID), databases.SystemPrincipal)
	if errors.Is(err, databases.ErrDuplicateKey) {
		return domainusers.ErrEmailAlreadyRegistered
	}
	return err
}

func (r *DocumentRepository) getByAttr(ctx context.Context, projectID, attr, value string) (*domainusers.User, error) {
	if r == nil || r.docDB == nil || value == "" {
		return nil, nil
	}
	list, err := r.docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, domainusers.CollectionID, databases.Query{
		Queries:  []string{query.BuildEqual(attr, value)},
		PageSize: 1,
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	if list == nil || len(list.Documents) == 0 {
		return nil, nil
	}
	return userFromDocument(&list.Documents[0]), nil
}

func documentPermissions(userID string) []databases.Permission {
	aces := domainusers.DocumentPermissions(userID)
	out := make([]databases.Permission, len(aces))
	for i, a := range aces {
		out[i] = databases.Permission{Type: a.Type, Role: a.Role}
	}
	return out
}

func userFromDocument(doc *databases.Document) *domainusers.User {
	if doc == nil {
		return nil
	}
	u := &domainusers.User{
		ID:            doc.ID,
		Email:         stringValue(doc.Data["email"]),
		PasswordHash:  stringValue(doc.Data["password_hash"]),
		Name:          stringValue(doc.Data["name"]),
		Status:        stringValue(doc.Data["status"]),
		EmailVerified: boolValue(doc.Data["email_verified"]),
		Phone:         stringValue(doc.Data["phone"]),
		PhoneVerified: boolValue(doc.Data["phone_verified"]),
		Labels:        labelsFromData(doc.Data["labels"]),
		PendingEmail:  stringValue(doc.Data["pending_email"]),
		CreatedAt:     doc.CreatedAt,
		UpdatedAt:     doc.UpdatedAt,
	}
	if prefs, ok := doc.Data["prefs"].(map[string]any); ok {
		u.Prefs = prefs
	}
	return u
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func labelsFromData(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		return domainusers.LabelsFromAny(v)
	default:
		return nil
	}
}
