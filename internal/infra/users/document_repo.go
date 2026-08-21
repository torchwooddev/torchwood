package users

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainusers "github.com/torchwooddev/torchwood/internal/domain/users"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DocumentRepository 把 User 聚合落到项目数据面系统集合 users（sentinel `_`）。
type DocumentRepository struct {
	docDB databases.DocumentDB
}

func NewDocumentRepository(docDB databases.DocumentDB) *DocumentRepository {
	return &DocumentRepository{docDB: docDB}
}

var (
	_ domainusers.Repository = (*DocumentRepository)(nil)

	errNilStore = errors.New("users document repository has no store")
)

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
		return errNilStore
	}
	if user == nil || strings.TrimSpace(user.ID) == "" {
		return domainusers.ErrUserIDRequired
	}
	doc := databases.Document{ID: user.ID, Data: user.DocumentData()}
	_, err := r.docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, domainusers.CollectionID, doc, documentPermissions(user.ID), databases.SystemPrincipal)
	return mapInsertDuplicate(err)
}

func mapInsertDuplicate(err error) error {
	if err == nil || !errors.Is(err, databases.ErrDuplicateKey) {
		return err
	}
	if emailUniqueViolation(err.Error()) {
		return domainusers.ErrEmailAlreadyRegistered
	}
	return err
}

func emailUniqueViolation(msg string) bool {
	return domainusers.IsEmailUniqueViolation(msg)
}

func (r *DocumentRepository) Update(ctx context.Context, projectID, id string, cols map[string]any) error {
	if r == nil || r.docDB == nil {
		return errNilStore
	}
	if strings.TrimSpace(id) == "" {
		return domainusers.ErrUserIDRequired
	}
	cols, err := domainusers.NormalizeUpdateColumns(cols)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	data, err := documentUpdateData(cols)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if len(data) == 0 {
		return status.Error(codes.InvalidArgument, domainusers.ErrInvalidUpdate.Error()+": no columns to update")
	}
	_, err = r.docDB.UpdateDocument(ctx, projectID, databases.SystemDatabaseID, domainusers.CollectionID,
		databases.SimpleDocumentUpdate(databases.Document{ID: id, Data: data}, nil), databases.SystemPrincipal)
	return mapInsertDuplicate(err)
}

func (r *DocumentRepository) Delete(ctx context.Context, projectID, id string) error {
	if r == nil || r.docDB == nil {
		return errNilStore
	}
	if strings.TrimSpace(id) == "" {
		return domainusers.ErrUserIDRequired
	}
	return r.docDB.DeleteDocument(ctx, projectID, databases.SystemDatabaseID, domainusers.CollectionID, id, databases.DeleteOptions{}, databases.SystemPrincipal)
}

func (r *DocumentRepository) List(ctx context.Context, projectID string, f domainusers.ListFilter) (*domainusers.ListResult, error) {
	if r == nil || r.docDB == nil {
		return nil, errNilStore
	}
	if _, err := domainusers.ParseUserList(f.Queries); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	list, err := r.docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, domainusers.CollectionID, databases.Query{
		Queries:   f.Queries,
		PageSize:  f.PageSize,
		PageToken: f.PageToken,
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	out := &domainusers.ListResult{}
	if list == nil {
		return out, nil
	}
	out.TotalCount = list.TotalCount
	out.NextPageToken = list.NextPageToken
	out.Users = make([]*domainusers.User, 0, len(list.Documents))
	for i := range list.Documents {
		out.Users = append(out.Users, userFromDocument(&list.Documents[i]))
	}
	return out, nil
}

func (r *DocumentRepository) UpdateFactors(ctx context.Context, projectID, id string, mutate func(current json.RawMessage) (json.RawMessage, error)) error {
	if r == nil || r.docDB == nil {
		return errNilStore
	}
	if strings.TrimSpace(id) == "" {
		return domainusers.ErrUserIDRequired
	}
	if mutate == nil {
		return status.Error(codes.InvalidArgument, "factors mutate is required")
	}
	user, err := r.GetByID(ctx, projectID, id)
	if err != nil {
		return err
	}
	if user == nil {
		return status.Error(codes.NotFound, "user not found")
	}
	current := user.Factors
	if len(current) == 0 {
		current = json.RawMessage(`{}`)
	}
	next, err := mutate(current)
	if err != nil {
		return err
	}
	if len(next) == 0 {
		next = json.RawMessage(`{}`)
	}
	var decoded any
	if err := json.Unmarshal(next, &decoded); err != nil {
		return status.Error(codes.InvalidArgument, "factors must be valid JSON")
	}
	_, err = r.docDB.UpdateDocument(ctx, projectID, databases.SystemDatabaseID, domainusers.CollectionID,
		databases.SimpleDocumentUpdate(databases.Document{ID: id, Data: map[string]any{"factors": decoded}}, nil), databases.SystemPrincipal)
	return err
}

func documentUpdateData(cols map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(cols))
	for k, v := range cols {
		if k == "updated_at" {
			continue
		}
		decoded, err := decodeJSONIfRaw(v)
		if err != nil {
			return nil, err
		}
		out[k] = decoded
	}
	return out, nil
}

func decodeJSONIfRaw(v any) (any, error) {
	switch t := v.(type) {
	case json.RawMessage:
		if len(t) == 0 {
			return map[string]any{}, nil
		}
		var decoded any
		if err := json.Unmarshal(t, &decoded); err != nil {
			return nil, err
		}
		return decoded, nil
	case []byte:
		if len(t) == 0 {
			return map[string]any{}, nil
		}
		var decoded any
		if err := json.Unmarshal(t, &decoded); err != nil {
			return nil, err
		}
		return decoded, nil
	default:
		return v, nil
	}
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
	u.Factors = factorsFromData(doc.Data["factors"])
	return u
}

func factorsFromData(raw any) json.RawMessage {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case json.RawMessage:
		return append(json.RawMessage(nil), v...)
	case []byte:
		return json.RawMessage(append([]byte(nil), v...))
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return b
	}
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
