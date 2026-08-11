package torchwood

import (
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
)

// HealthService 封装 Server API 的健康检查服务。
type HealthService struct{ c *ServerClient }

// Check 返回服务与底座依赖的健康状态。
func (h *HealthService) Check(ctx context.Context) (*serverv1.HealthCheckResponse, error) {
	return h.c.health.Check(h.c.AuthContext(ctx), &serverv1.HealthCheckRequest{})
}

// Version 返回服务版本信息。
func (h *HealthService) Version(ctx context.Context) (*serverv1.GetVersionResponse, error) {
	return h.c.health.GetVersion(h.c.AuthContext(ctx), &serverv1.GetVersionRequest{})
}

// UsersService 封装 Server API 的用户管理服务。
type UsersService struct{ c *ServerClient }

// CreateUser 创建用户（Agent 账号也走此接口）。
func (u *UsersService) CreateUser(ctx context.Context, email, password, name, status string, labels, prefs map[string]any) (*serverv1.User, error) {
	labelStruct, err := toStruct(labels)
	if err != nil {
		return nil, err
	}
	prefsStruct, err := toStruct(prefs)
	if err != nil {
		return nil, err
	}
	return u.c.users.CreateUser(u.c.AuthContext(ctx), &serverv1.CreateUserRequest{
		Email:    email,
		Password: password,
		Name:     name,
		Status:   status,
		Labels:   labelStruct,
		Prefs:    prefsStruct,
	})
}

// GetUser 按 ID 获取用户。
func (u *UsersService) GetUser(ctx context.Context, userID string) (*serverv1.User, error) {
	return u.c.users.GetUser(u.c.AuthContext(ctx), &serverv1.GetUserRequest{Id: userID})
}

// ListUsers 按查询 DSL 列出用户。
func (u *UsersService) ListUsers(ctx context.Context, queries []string, pageSize int32, pageToken string) (*serverv1.ListUsersResponse, error) {
	return u.c.users.ListUsers(u.c.AuthContext(ctx), &sharedv1.ListRequest{
		PageSize:  pageSize,
		PageToken: pageToken,
		Queries:   queries,
	})
}

// UpdateUser 更新用户档案字段。
func (u *UsersService) UpdateUser(ctx context.Context, userID, name, email, status string, labels, prefs map[string]any) (*serverv1.User, error) {
	labelStruct, err := toStruct(labels)
	if err != nil {
		return nil, err
	}
	prefsStruct, err := toStruct(prefs)
	if err != nil {
		return nil, err
	}
	return u.c.users.UpdateUser(u.c.AuthContext(ctx), &serverv1.UpdateUserRequest{
		Id:     userID,
		Name:   name,
		Email:  email,
		Status: status,
		Labels: labelStruct,
		Prefs:  prefsStruct,
	})
}

// DeleteUser 删除用户。
func (u *UsersService) DeleteUser(ctx context.Context, userID string) error {
	_, err := u.c.users.DeleteUser(u.c.AuthContext(ctx), &serverv1.GetUserRequest{Id: userID})
	return err
}

// ListUserSessions 列出用户会话。
func (u *UsersService) ListUserSessions(ctx context.Context, userID string) (*serverv1.ListUserSessionsResponse, error) {
	return u.c.users.ListUserSessions(u.c.AuthContext(ctx), &serverv1.GetUserRequest{Id: userID})
}

// DeleteUserSession 删除指定用户会话（Agent 撤权三重生效的一环）。
func (u *UsersService) DeleteUserSession(ctx context.Context, userID, sessionID string) error {
	_, err := u.c.users.DeleteUserSession(u.c.AuthContext(ctx), &serverv1.DeleteUserSessionRequest{
		Id:        userID,
		SessionId: sessionID,
	})
	return err
}

// CreateUserToken 为任意用户签发 client token（如 Agent 登录凭证）。
func (u *UsersService) CreateUserToken(ctx context.Context, userID string) (*serverv1.CreateUserTokenResponse, error) {
	return u.c.users.CreateUserToken(u.c.AuthContext(ctx), &serverv1.GetUserRequest{Id: userID})
}

// ServerTeamsService 封装 Server API 的团队管理服务。
type ServerTeamsService struct{ c *ServerClient }

// CreateTeam 创建团队（可附带权限）。
func (t *ServerTeamsService) CreateTeam(ctx context.Context, name string, permissions []string) (*serverv1.Team, error) {
	return t.c.teams.CreateTeam(t.c.AuthContext(ctx), &serverv1.CreateTeamRequest{
		Name:        name,
		Permissions: permissions,
	})
}

// GetTeam 按 ID 获取团队。
func (t *ServerTeamsService) GetTeam(ctx context.Context, teamID string) (*serverv1.Team, error) {
	return t.c.teams.GetTeam(t.c.AuthContext(ctx), &serverv1.GetTeamRequest{Id: teamID})
}

// ListTeams 按查询 DSL 列出团队。
func (t *ServerTeamsService) ListTeams(ctx context.Context, queries []string, pageSize int32, pageToken string) (*serverv1.ListTeamsResponse, error) {
	return t.c.teams.ListTeams(t.c.AuthContext(ctx), &sharedv1.ListRequest{
		PageSize:  pageSize,
		PageToken: pageToken,
		Queries:   queries,
	})
}

// CreateMembership 创建团队成员关系（按 userID 或邮箱）。
func (t *ServerTeamsService) CreateMembership(ctx context.Context, teamID, userID, email, name string, roles []string, status string) (*serverv1.Membership, error) {
	return t.c.teams.CreateMembership(t.c.AuthContext(ctx), &serverv1.CreateMembershipRequest{
		TeamId: teamID,
		UserId: userID,
		Email:  email,
		Name:   name,
		Roles:  roles,
		Status: status,
	})
}

// ListMemberships 列出团队成员。
func (t *ServerTeamsService) ListMemberships(ctx context.Context, teamID string) (*serverv1.ListMembershipsResponse, error) {
	return t.c.teams.ListMemberships(t.c.AuthContext(ctx), &serverv1.ListMembershipsRequest{TeamId: teamID})
}

// GetMembership 获取成员关系。
func (t *ServerTeamsService) GetMembership(ctx context.Context, teamID, membershipID string) (*serverv1.Membership, error) {
	return t.c.teams.GetMembership(t.c.AuthContext(ctx), &serverv1.GetMembershipRequest{
		TeamId:       teamID,
		MembershipId: membershipID,
	})
}

// UpdateMembership 更新成员角色。
func (t *ServerTeamsService) UpdateMembership(ctx context.Context, teamID, membershipID string, roles []string) (*serverv1.Membership, error) {
	return t.c.teams.UpdateMembership(t.c.AuthContext(ctx), &serverv1.UpdateMembershipRequest{
		TeamId:       teamID,
		MembershipId: membershipID,
		Roles:        roles,
	})
}

// UpdateMembershipStatus 更新成员状态。
func (t *ServerTeamsService) UpdateMembershipStatus(ctx context.Context, teamID, membershipID, status string) (*serverv1.Membership, error) {
	return t.c.teams.UpdateMembershipStatus(t.c.AuthContext(ctx), &serverv1.UpdateMembershipStatusRequest{
		TeamId:       teamID,
		MembershipId: membershipID,
		Status:       status,
	})
}

// DeleteMembership 移除成员关系。
func (t *ServerTeamsService) DeleteMembership(ctx context.Context, teamID, membershipID string) error {
	_, err := t.c.teams.DeleteMembership(t.c.AuthContext(ctx), &serverv1.GetMembershipRequest{
		TeamId:       teamID,
		MembershipId: membershipID,
	})
	return err
}

// ServerDatabasesService 封装 Server API 的数据库管理服务
// （库/集合/属性/索引/文档，含 upsert 与批量操作）。
// 默认数据库见 [WithDatabaseID]，可用 [ServerClient.UseDatabase] 切换。
type ServerDatabasesService struct {
	c  *ServerClient
	db string
}

// CreateDatabase 创建数据库。
func (d *ServerDatabasesService) CreateDatabase(ctx context.Context, id, name string) (*serverv1.Database, error) {
	return d.c.databases.CreateDatabase(d.c.AuthContext(ctx), &serverv1.CreateDatabaseRequest{
		Id:   id,
		Name: name,
	})
}

// GetDatabase 获取数据库。
func (d *ServerDatabasesService) GetDatabase(ctx context.Context, id string) (*serverv1.Database, error) {
	return d.c.databases.GetDatabase(d.c.AuthContext(ctx), &serverv1.GetDatabaseRequest{Id: id})
}

// ListDatabases 列出数据库。
func (d *ServerDatabasesService) ListDatabases(ctx context.Context, queries []string, pageSize int32, pageToken string) (*serverv1.ListDatabasesResponse, error) {
	return d.c.databases.ListDatabases(d.c.AuthContext(ctx), &sharedv1.ListRequest{
		PageSize:  pageSize,
		PageToken: pageToken,
		Queries:   queries,
	})
}

// CreateCollection 创建集合。documentSecurity 开启文档级权限。
func (d *ServerDatabasesService) CreateCollection(ctx context.Context, collectionID, name string, permissions []string, documentSecurity bool) (*serverv1.Collection, error) {
	return d.c.databases.CreateCollection(d.c.AuthContext(ctx), &serverv1.CreateCollectionRequest{
		DatabaseId:       d.db,
		Id:               collectionID,
		Name:             name,
		Permissions:      permissions,
		DocumentSecurity: &documentSecurity,
	})
}

// GetCollection 获取集合。
func (d *ServerDatabasesService) GetCollection(ctx context.Context, collectionID string) (*serverv1.Collection, error) {
	return d.c.databases.GetCollection(d.c.AuthContext(ctx), &serverv1.GetCollectionRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
	})
}

// ListCollections 按查询 DSL 列出集合。
func (d *ServerDatabasesService) ListCollections(ctx context.Context, queries []string, pageSize int32, pageToken string) (*serverv1.ListCollectionsResponse, error) {
	return d.c.databases.ListCollections(d.c.AuthContext(ctx), &serverv1.ListCollectionsRequest{
		DatabaseId: d.db,
		Queries:    queries,
		PageSize:   pageSize,
		PageToken:  pageToken,
	})
}

// CreateAttribute 为集合添加属性（type 支持 string/integer/float/boolean/datetime/email/url/json 等）。
func (d *ServerDatabasesService) CreateAttribute(ctx context.Context, collectionID, key, attrType string, size int32, required, array bool) (*serverv1.Attribute, error) {
	return d.c.databases.CreateAttribute(d.c.AuthContext(ctx), &serverv1.CreateAttributeRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		Key:          key,
		Type:         attrType,
		Size:         size,
		Required:     required,
		Array:        array,
	})
}

// CreateIndex 为集合创建索引（type 支持 key/unique/fulltext）。
func (d *ServerDatabasesService) CreateIndex(ctx context.Context, collectionID, id, indexType string, attributes []string) (*serverv1.Index, error) {
	return d.c.databases.CreateIndex(d.c.AuthContext(ctx), &serverv1.CreateIndexRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		Id:           id,
		Type:         indexType,
		Attributes:   attributes,
	})
}

// CreateDocument 在集合中创建文档。
func (d *ServerDatabasesService) CreateDocument(ctx context.Context, collectionID, documentID string, data map[string]any, permissions []string) (*serverv1.Document, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.CreateDocument(d.c.AuthContext(ctx), &serverv1.CreateDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
		Data:         st,
		Permissions:  permissions,
	})
}

// GetDocument 读取文档，不存在时返回 codes.NotFound。
func (d *ServerDatabasesService) GetDocument(ctx context.Context, collectionID, documentID string) (*serverv1.Document, error) {
	return d.c.databases.GetDocument(d.c.AuthContext(ctx), &serverv1.GetDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
	})
}

// UpdateDocument 更新文档字段；increment 对数字字段做原子增量。
func (d *ServerDatabasesService) UpdateDocument(ctx context.Context, collectionID, documentID string, data map[string]any, increment map[string]int64, permissions []string) (*serverv1.Document, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.UpdateDocument(d.c.AuthContext(ctx), &serverv1.UpdateDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
		Data:         st,
		Permissions:  permissions,
		Increment:    increment,
	})
}

// UpsertDocument 按 conflictColumns（须匹配集合唯一索引）插入或更新文档。
func (d *ServerDatabasesService) UpsertDocument(ctx context.Context, collectionID, documentID string, data map[string]any, conflictColumns, permissions []string) (*serverv1.Document, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.UpsertDocument(d.c.AuthContext(ctx), &serverv1.UpsertDocumentRequest{
		DatabaseId:      d.db,
		CollectionId:    collectionID,
		DocumentId:      documentID,
		Data:            st,
		Permissions:     permissions,
		ConflictColumns: conflictColumns,
	})
}

// DeleteDocument 删除文档。
func (d *ServerDatabasesService) DeleteDocument(ctx context.Context, collectionID, documentID string) error {
	_, err := d.c.databases.DeleteDocument(d.c.AuthContext(ctx), &serverv1.GetDocumentRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentId:   documentID,
	})
	return err
}

// ListDocuments 按查询 DSL 列出文档，返回文档列表与下一页游标（空表示无更多）。
func (d *ServerDatabasesService) ListDocuments(ctx context.Context, collectionID string, queries []string, pageSize int32, pageToken string) ([]*serverv1.Document, string, error) {
	resp, err := d.c.databases.ListDocuments(d.c.AuthContext(ctx), &serverv1.ListDocumentsRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		Queries:      queries,
		PageSize:     pageSize,
		PageToken:    pageToken,
	})
	if err != nil {
		return nil, "", err
	}
	var next string
	if resp.Meta != nil {
		next = resp.Meta.NextPageToken
	}
	return resp.Documents, next, nil
}

// CountDocuments 按查询 DSL 统计文档数量。
func (d *ServerDatabasesService) CountDocuments(ctx context.Context, collectionID string, queries []string) (int64, error) {
	resp, err := d.c.databases.CountDocuments(d.c.AuthContext(ctx), &serverv1.ListDocumentsRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		Queries:      queries,
	})
	if err != nil {
		return 0, err
	}
	return resp.Count, nil
}

// BulkUpdateDocuments 批量更新文档，返回受影响数量。
func (d *ServerDatabasesService) BulkUpdateDocuments(ctx context.Context, collectionID string, documentIDs []string, data map[string]any, permissions []string) (*serverv1.BulkDocumentsResponse, error) {
	st, err := toStruct(data)
	if err != nil {
		return nil, err
	}
	return d.c.databases.BulkUpdateDocuments(d.c.AuthContext(ctx), &serverv1.BulkUpdateDocumentsRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentIds:  documentIDs,
		Data:         st,
		Permissions:  permissions,
	})
}

// BulkDeleteDocuments 批量删除文档，返回受影响数量。
func (d *ServerDatabasesService) BulkDeleteDocuments(ctx context.Context, collectionID string, documentIDs []string) (*serverv1.BulkDocumentsResponse, error) {
	return d.c.databases.BulkDeleteDocuments(d.c.AuthContext(ctx), &serverv1.BulkDeleteDocumentsRequest{
		DatabaseId:   d.db,
		CollectionId: collectionID,
		DocumentIds:  documentIDs,
	})
}
