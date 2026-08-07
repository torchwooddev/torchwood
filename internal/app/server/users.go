package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/teams"
	"github.com/torchwooddev/torchwood/internal/domain/users"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Users struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB
}

func NewUsers(projectRepo projects.Repository, docDB databases.DocumentDB) *Users {
	return &Users{projectRepo: projectRepo, docDB: docDB}
}

func (u *Users) resolveProject(ctx context.Context, projectID string) (*projects.Project, error) {
	p, err := u.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	if err := u.docDB.EnsureSystemCollections(ctx, p.ID, p.InternalID); err != nil {
		return nil, err
	}
	return p, nil
}

func (u *Users) ListUsers(ctx context.Context, projectID string, q databases.Query, principal databases.Principal) ([]databases.Document, int64, string, error) {
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, 0, "", err
	}
	list, err := u.docDB.ListDocuments(ctx, projectID, "default", "users", q, principal)
	if err != nil {
		return nil, 0, "", err
	}
	return list.Documents, list.TotalCount, list.NextPageToken, nil
}

func (u *Users) GetUser(ctx context.Context, projectID, userID string, principal databases.Principal) (*databases.Document, error) {
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return u.docDB.GetDocument(ctx, projectID, "default", "users", userID, principal)
}

var userUpdateProtectedFields = map[string]struct{}{
	"password_hash":  {},
	"email_verified": {},
	"status":         {},
}

func (u *Users) UpdateUser(ctx context.Context, projectID, userID string, updates map[string]any, principal databases.Principal) (*databases.Document, error) {
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	if raw, ok := updates["status"]; ok {
		s, ok := raw.(string)
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "status must be a string")
		}
		if err := users.ValidateStatus(s); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	filtered := make(map[string]any, len(updates))
	for k, v := range updates {
		if strings.HasPrefix(k, "_") {
			continue
		}
		if _, blocked := userUpdateProtectedFields[k]; blocked {
			continue
		}
		filtered[k] = v
	}
	if v, ok := filtered["email"].(string); ok && v != "" {
		filtered["email_verified"] = false
	}
	if len(filtered) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no updatable fields supplied (password_hash, email_verified, status are managed via dedicated endpoints)")
	}
	// 用例层即权限层：keys 角色已由拦截器 scope 把关，docDB 调用统一走 SystemPrincipal，
	// 避免非 System 主体触发系统集合写保护（安全评审 C1 方案 (a)）。
	doc := databases.Document{ID: userID, Data: filtered}
	updated, err := u.docDB.UpdateDocument(ctx, projectID, "default", "users", databases.SimpleDocumentUpdate(doc, nil), databases.SystemPrincipal)
	if err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	return &updated, nil
}

func (u *Users) DeleteUser(ctx context.Context, projectID, userID string, principal databases.Principal) error {
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return err
	}
	// M10：删除 users 文档前级联清理 sessions/identities/memberships，
	// 避免 identity 残留阻塞同 provider 重新注册、memberships 残留遗留孤儿团队角色。
	if err := u.deleteUserCascade(ctx, projectID, userID); err != nil {
		return err
	}
	return u.docDB.DeleteDocument(ctx, projectID, "default", "users", userID, databases.SystemPrincipal)
}

// deleteUserCascade 以 SystemPrincipal 清理用户的 sessions/identities/memberships，
// 并按 team_id 聚合递减 teams 文档 total（与 Teams.adjustTeamTotal 语义一致，
// 内联实现避免跨用例结构体耦合）。
func (u *Users) deleteUserCascade(ctx context.Context, projectID, userID string) error {
	for _, coll := range []string{"sessions", "identities"} {
		list, err := u.docDB.ListDocuments(ctx, projectID, "default", coll, databases.Query{
			Queries: []string{query.BuildEqual("user_id", userID)},
		}, databases.SystemPrincipal)
		if err != nil {
			return fmt.Errorf("list %s for user: %w", coll, err)
		}
		for _, doc := range list.Documents {
			if err := u.docDB.DeleteDocument(ctx, projectID, "default", coll, doc.ID, databases.SystemPrincipal); err != nil {
				return fmt.Errorf("delete %s: %w", coll, err)
			}
		}
	}

	// memberships：仅 accepted 状态计入团队 total（与 CreateMembership/DeleteMembership 一致）。
	teamsToAdjust := map[string]struct{}{}
	list, err := u.docDB.ListDocuments(ctx, projectID, "default", "memberships", databases.Query{
		Queries: []string{query.BuildEqual("user_id", userID)},
	}, databases.SystemPrincipal)
	if err != nil {
		return fmt.Errorf("list memberships for user: %w", err)
	}
	for _, doc := range list.Documents {
		if statusVal, _ := doc.Data["status"].(string); statusVal == teams.StatusAccepted {
			if teamID, _ := doc.Data["team_id"].(string); teamID != "" {
				teamsToAdjust[teamID] = struct{}{}
			}
		}
		if err := u.docDB.DeleteDocument(ctx, projectID, "default", "memberships", doc.ID, databases.SystemPrincipal); err != nil {
			return fmt.Errorf("delete membership: %w", err)
		}
	}
	for teamID := range teamsToAdjust {
		if err := u.adjustTeamTotal(ctx, projectID, teamID, -1); err != nil {
			return err
		}
	}
	return nil
}

// adjustTeamTotal 递增/递减团队 total（与 Teams.adjustTeamTotal 逻辑一致，
// 此处针对 Users 内联实现，避免跨用例结构体依赖）。
func (u *Users) adjustTeamTotal(ctx context.Context, projectID, teamID string, delta int) error {
	doc, err := u.docDB.GetDocument(ctx, projectID, "default", "teams", teamID, databases.SystemPrincipal)
	if err != nil {
		return err
	}
	if doc == nil {
		return nil
	}
	total := int64(0)
	switch v := doc.Data["total"].(type) {
	case float64:
		total = int64(v)
	case int64:
		total = v
	case int:
		total = int64(v)
	}
	total += int64(delta)
	if total < 0 {
		total = 0
	}
	_, err = u.docDB.UpdateDocument(ctx, projectID, "default", "teams", databases.SimpleDocumentUpdate(databases.Document{
		ID:   teamID,
		Data: map[string]any{"total": total},
	}, nil), databases.SystemPrincipal)
	return err
}

func (u *Users) UpdateUserStatus(ctx context.Context, projectID, userID, userStatus string, principal databases.Principal) (*databases.Document, error) {
	if userStatus == "" {
		userStatus = users.StatusActive
	}
	if err := users.ValidateStatus(userStatus); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := u.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	updated, err := u.docDB.UpdateDocument(ctx, projectID, "default", "users", databases.SimpleDocumentUpdate(databases.Document{
		ID:   userID,
		Data: map[string]any{"status": userStatus, "updated_at": time.Now().Format(time.RFC3339Nano)},
	}, nil), databases.SystemPrincipal)
	if err != nil {
		return nil, fmt.Errorf("update user status: %w", err)
	}
	return &updated, nil
}
