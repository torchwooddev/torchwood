package server

import (
	"context"
	"fmt"
	"time"

	appshared "github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/teams"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Teams struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB
}

func NewTeams(projectRepo projects.Repository, docDB databases.DocumentDB) *Teams {
	return &Teams{projectRepo: projectRepo, docDB: docDB}
}

type CreateMembershipCommand struct {
	TeamID string
	UserID string
	Email  string
	Name   string
	Roles  []string
	Status string
}

type UpdateMembershipCommand struct {
	Roles []string
}

func (t *Teams) resolveProject(ctx context.Context, projectID string) (*projects.Project, error) {
	p, err := t.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	if err := t.docDB.EnsureSystemCollections(ctx, p.ID, p.InternalID); err != nil {
		return nil, err
	}
	return p, nil
}

func (t *Teams) getTeamDoc(ctx context.Context, projectID, teamID string, principal databases.Principal) (*databases.Document, error) {
	doc, err := t.docDB.GetDocument(ctx, projectID, "default", "teams", teamID, principal)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "team not found")
	}
	return doc, nil
}

func (t *Teams) CreateTeam(ctx context.Context, projectID, name string, perms []string) (*databases.Document, error) {
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if _, err := t.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	teamID := idgen.UUID().String()
	doc := databases.Document{
		ID: teamID,
		Data: map[string]any{
			"name":        name,
			"permissions": perms,
			"total":       0,
		},
	}
	if _, err := t.docDB.CreateDocument(ctx, projectID, "default", "teams", doc, defaultTeamPermissions(teamID, perms), databases.SystemPrincipal); err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	return t.docDB.GetDocument(ctx, projectID, "default", "teams", teamID, databases.SystemPrincipal)
}

func (t *Teams) CreateTeamWithOwner(ctx context.Context, projectID, name, ownerUserID, ownerEmail string, principal databases.Principal) (*databases.Document, *databases.Document, error) {
	team, err := t.CreateTeam(ctx, projectID, name, nil)
	if err != nil {
		return nil, nil, err
	}
	membership, err := t.CreateMembership(ctx, projectID, CreateMembershipCommand{
		TeamID: team.ID,
		UserID: ownerUserID,
		Email:  ownerEmail,
		Roles:  []string{teams.RoleOwner},
		Status: teams.StatusAccepted,
	}, principal)
	if err != nil {
		_ = t.docDB.DeleteDocument(ctx, projectID, "default", "teams", team.ID, databases.SystemPrincipal)
		return nil, nil, err
	}
	team, err = t.GetTeam(ctx, projectID, team.ID, databases.SystemPrincipal)
	if err != nil {
		return nil, nil, err
	}
	return team, membership, nil
}

func (t *Teams) ListTeams(ctx context.Context, projectID string, q databases.Query, principal databases.Principal) ([]databases.Document, int64, string, error) {
	if _, err := t.resolveProject(ctx, projectID); err != nil {
		return nil, 0, "", err
	}
	list, err := t.docDB.ListDocuments(ctx, projectID, "default", "teams", q, principal)
	if err != nil {
		return nil, 0, "", err
	}
	return list.Documents, list.TotalCount, list.NextPageToken, nil
}

func (t *Teams) GetTeam(ctx context.Context, projectID, teamID string, principal databases.Principal) (*databases.Document, error) {
	if _, err := t.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	return t.docDB.GetDocument(ctx, projectID, "default", "teams", teamID, principal)
}

func (t *Teams) GetTeamPrefs(ctx context.Context, projectID, teamID string, principal databases.Principal) (map[string]any, error) {
	if _, err := t.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	doc, err := t.getTeamDoc(ctx, projectID, teamID, principal)
	if err != nil {
		return nil, err
	}
	if prefs, ok := doc.Data["prefs"].(map[string]any); ok {
		return prefs, nil
	}
	return map[string]any{}, nil
}

func (t *Teams) UpdateTeamPrefs(ctx context.Context, projectID, teamID string, prefs map[string]any, principal databases.Principal) (map[string]any, error) {
	if _, err := t.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	if prefs == nil {
		return nil, status.Error(codes.InvalidArgument, "prefs is required")
	}
	if _, err := t.getTeamDoc(ctx, projectID, teamID, principal); err != nil {
		return nil, err
	}
	updated, err := t.docDB.UpdateDocument(ctx, projectID, "default", "teams", databases.SimpleDocumentUpdate(databases.Document{
		ID:   teamID,
		Data: map[string]any{"prefs": prefs},
	}, nil), principal)
	if err != nil {
		return nil, appshared.MapDocumentDBError(err)
	}
	if out, ok := updated.Data["prefs"].(map[string]any); ok {
		return out, nil
	}
	return map[string]any{}, nil
}

func (t *Teams) DeleteTeam(ctx context.Context, projectID, teamID string, principal databases.Principal) error {
	if _, err := t.resolveProject(ctx, projectID); err != nil {
		return err
	}
	if _, err := t.getTeamDoc(ctx, projectID, teamID, principal); err != nil {
		return err
	}
	memberships, err := t.listMembershipDocs(ctx, projectID, teamID, databases.SystemPrincipal)
	if err != nil {
		return err
	}
	for _, m := range memberships {
		if err := t.docDB.DeleteDocument(ctx, projectID, "default", "memberships", m.ID, databases.SystemPrincipal); err != nil {
			return err
		}
	}
	return t.docDB.DeleteDocument(ctx, projectID, "default", "teams", teamID, principal)
}

func (t *Teams) CreateMembership(ctx context.Context, projectID string, cmd CreateMembershipCommand, principal databases.Principal) (*databases.Document, error) {
	if cmd.TeamID == "" {
		return nil, status.Error(codes.InvalidArgument, "team_id is required")
	}
	if cmd.UserID == "" && cmd.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id or email is required")
	}
	if _, err := t.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	if _, err := t.getTeamDoc(ctx, projectID, cmd.TeamID, principalOrSystem(principal)); err != nil {
		return nil, err
	}

	statusVal := cmd.Status
	if statusVal == "" {
		statusVal = teams.StatusPending
	}
	if err := teams.ValidateStatus(statusVal); err != nil {
		return nil, err
	}
	membershipRoles := cmd.Roles
	if len(membershipRoles) == 0 {
		membershipRoles = []string{teams.RoleMember}
	}
	for _, role := range membershipRoles {
		if err := teams.ValidateRole(role); err != nil {
			return nil, err
		}
	}

	userID := cmd.UserID
	if userID == "" && statusVal == teams.StatusAccepted {
		return nil, status.Error(codes.InvalidArgument, "user_id is required for accepted membership")
	}
	if userID == "" && cmd.Email != "" {
		resolved, err := t.resolveUserIDByEmail(ctx, projectID, cmd.Email)
		if err != nil {
			return nil, err
		}
		userID = resolved
	}

	now := time.Now()
	data := map[string]any{
		"team_id":    cmd.TeamID,
		"user_id":    userID,
		"email":      cmd.Email,
		"name":       cmd.Name,
		"roles":      membershipRoles,
		"status":     statusVal,
		"invited_at": now.Format(time.RFC3339Nano),
	}
	if statusVal == teams.StatusAccepted {
		data["joined_at"] = now.Format(time.RFC3339Nano)
	}

	membershipID := idgen.UUID().String()
	created, err := t.docDB.CreateDocument(ctx, projectID, "default", "memberships", databases.Document{
		ID:   membershipID,
		Data: data,
	}, membershipPermissions(cmd.TeamID, userID), principal)
	if err != nil {
		return nil, fmt.Errorf("create membership: %w", err)
	}
	if statusVal == teams.StatusAccepted {
		if err := t.adjustTeamTotal(ctx, projectID, cmd.TeamID, 1, principalOrSystem(principal)); err != nil {
			return nil, err
		}
	}
	return t.docDB.GetDocument(ctx, projectID, "default", "memberships", created.ID, databases.SystemPrincipal)
}

func (t *Teams) ListMemberships(ctx context.Context, projectID, teamID string, q databases.Query, principal databases.Principal) ([]databases.Document, int64, string, error) {
	if _, err := t.resolveProject(ctx, projectID); err != nil {
		return nil, 0, "", err
	}
	if _, err := t.getTeamDoc(ctx, projectID, teamID, principalOrSystem(principal)); err != nil {
		return nil, 0, "", err
	}
	queries := append([]string{query.BuildEqual("team_id", teamID)}, q.Queries...)
	list, err := t.docDB.ListDocuments(ctx, projectID, "default", "memberships", databases.Query{
		Queries:   queries,
		PageSize:  q.PageSize,
		PageToken: q.PageToken,
	}, principal)
	if err != nil {
		return nil, 0, "", err
	}
	return list.Documents, list.TotalCount, list.NextPageToken, nil
}

func (t *Teams) GetMembership(ctx context.Context, projectID, teamID, membershipID string, principal databases.Principal) (*databases.Document, error) {
	return t.getMembershipDoc(ctx, projectID, teamID, membershipID, principal)
}

func (t *Teams) UpdateMembership(ctx context.Context, projectID, teamID, membershipID string, cmd UpdateMembershipCommand, principal databases.Principal) (*databases.Document, error) {
	if len(cmd.Roles) == 0 {
		return nil, status.Error(codes.InvalidArgument, "roles is required")
	}
	for _, role := range cmd.Roles {
		if err := teams.ValidateRole(role); err != nil {
			return nil, err
		}
	}
	doc, err := t.getMembershipDoc(ctx, projectID, teamID, membershipID, principal)
	if err != nil {
		return nil, err
	}
	// last-owner 保护：降级 owner 角色前校验团队仍有其他 owner。
	if !containsRole(cmd.Roles, teams.RoleOwner) {
		if err := t.guardLastOwner(ctx, projectID, teamID, doc); err != nil {
			return nil, err
		}
	}
	updated, err := t.docDB.UpdateDocument(ctx, projectID, "default", "memberships", databases.SimpleDocumentUpdate(databases.Document{
		ID:   membershipID,
		Data: map[string]any{"roles": cmd.Roles},
	}, nil), principal)
	if err != nil {
		return nil, fmt.Errorf("update membership: %w", err)
	}
	return &updated, nil
}

func (t *Teams) UpdateMembershipStatus(ctx context.Context, projectID, teamID, membershipID, statusVal string, principal databases.Principal) (*databases.Document, error) {
	if err := teams.ValidateStatus(statusVal); err != nil {
		return nil, err
	}
	if statusVal == teams.StatusPending {
		return nil, status.Error(codes.InvalidArgument, "cannot set status back to pending")
	}
	doc, err := t.getMembershipDoc(ctx, projectID, teamID, membershipID, principal)
	if err != nil {
		return nil, err
	}
	current, _ := doc.Data["status"].(string)
	if current != teams.StatusPending {
		return nil, status.Error(codes.FailedPrecondition, "membership status is not pending")
	}

	updates := map[string]any{"status": statusVal}
	userID, _ := doc.Data["user_id"].(string)
	if statusVal == teams.StatusAccepted {
		if userID == "" {
			email, _ := doc.Data["email"].(string)
			if email == "" {
				return nil, status.Error(codes.FailedPrecondition, "membership has no user to accept")
			}
			userID, err = t.resolveUserIDByEmail(ctx, projectID, email)
			if err != nil {
				return nil, err
			}
			if userID == "" {
				return nil, status.Error(codes.NotFound, "user not found for membership email")
			}
			updates["user_id"] = userID
		}
		updates["joined_at"] = time.Now().Format(time.RFC3339Nano)
	}

	var perms []databases.Permission
	if _, ok := updates["user_id"]; ok {
		perms = membershipPermissions(teamID, userID)
	}
	updated, err := t.docDB.UpdateDocument(ctx, projectID, "default", "memberships", databases.SimpleDocumentUpdate(databases.Document{
		ID:   membershipID,
		Data: updates,
	}, perms), principal)
	if err != nil {
		return nil, fmt.Errorf("update membership status: %w", err)
	}
	if statusVal == teams.StatusAccepted {
		if err := t.adjustTeamTotal(ctx, projectID, teamID, 1, principalOrSystem(principal)); err != nil {
			return nil, err
		}
	}
	return &updated, nil
}

func (t *Teams) DeleteMembership(ctx context.Context, projectID, teamID, membershipID string, principal databases.Principal) error {
	doc, err := t.getMembershipDoc(ctx, projectID, teamID, membershipID, principal)
	if err != nil {
		return err
	}
	// last-owner 保护：删除 owner membership 前校验团队仍有其他 owner
	// （client 自退路径经本用例同样受保护）。
	if err := t.guardLastOwner(ctx, projectID, teamID, doc); err != nil {
		return err
	}
	if statusVal, _ := doc.Data["status"].(string); statusVal == teams.StatusAccepted {
		if err := t.adjustTeamTotal(ctx, projectID, teamID, -1, principalOrSystem(principal)); err != nil {
			return err
		}
	}
	return t.docDB.DeleteDocument(ctx, projectID, "default", "memberships", membershipID, principal)
}

func (t *Teams) ListAcceptedTeamRoles(ctx context.Context, projectID, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	list, err := t.docDB.ListDocuments(ctx, projectID, "default", "memberships", databases.Query{
		Queries: []string{
			query.BuildEqual("user_id", userID),
			query.BuildEqual("status", teams.StatusAccepted),
		},
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, doc := range list.Documents {
		teamID, _ := doc.Data["team_id"].(string)
		if teamID == "" {
			continue
		}
		out = append(out, fmt.Sprintf("team:%s", teamID), fmt.Sprintf("member:%s", doc.ID))
	}
	return out, nil
}

// AcceptedTeamRoleLabels 返回用户在各团队中已接受 membership 的原始角色标签（如 owner/member），
// key 为 team_id。
func (t *Teams) AcceptedTeamRoleLabels(ctx context.Context, projectID, userID string) (map[string][]string, error) {
	if userID == "" {
		return nil, nil
	}
	list, err := t.docDB.ListDocuments(ctx, projectID, "default", "memberships", databases.Query{
		Queries: []string{
			query.BuildEqual("user_id", userID),
			query.BuildEqual("status", teams.StatusAccepted),
		},
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(list.Documents))
	for _, doc := range list.Documents {
		teamID, _ := doc.Data["team_id"].(string)
		if teamID == "" {
			continue
		}
		out[teamID] = membershipRoleLabels(doc.Data["roles"])
	}
	return out, nil
}

// membershipRoleLabels 将 membership 的 roles 字段（JSONB 反序列化后可能是 []any 或 []string）转换为 []string。
func membershipRoleLabels(raw any) []string {
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	default:
		return nil
	}
}

func (t *Teams) getMembershipDoc(ctx context.Context, projectID, teamID, membershipID string, principal databases.Principal) (*databases.Document, error) {
	if _, err := t.resolveProject(ctx, projectID); err != nil {
		return nil, err
	}
	doc, err := t.docDB.GetDocument(ctx, projectID, "default", "memberships", membershipID, principal)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, status.Error(codes.NotFound, "membership not found")
	}
	if gotTeamID, _ := doc.Data["team_id"].(string); gotTeamID != teamID {
		return nil, status.Error(codes.NotFound, "membership not found")
	}
	return doc, nil
}

func (t *Teams) listMembershipDocs(ctx context.Context, projectID, teamID string, _ databases.Principal) ([]databases.Document, error) {
	return cascadeListAll(ctx, t.docDB, projectID, "memberships", []string{query.BuildEqual("team_id", teamID)})
}

// guardLastOwner 在删除/降级 owner membership 前校验：目标为 accepted 且含
// owner 角色时，团队其余 accepted+owner 数量必须 ≥1，防止团队失去最后 owner。
func (t *Teams) guardLastOwner(ctx context.Context, projectID, teamID string, target *databases.Document) error {
	if statusVal, _ := target.Data["status"].(string); statusVal != teams.StatusAccepted {
		return nil
	}
	if !containsRole(membershipRoleLabels(target.Data["roles"]), teams.RoleOwner) {
		return nil
	}
	memberships, err := t.listMembershipDocs(ctx, projectID, teamID, databases.SystemPrincipal)
	if err != nil {
		return err
	}
	otherOwners := 0
	for _, m := range memberships {
		if m.ID == target.ID {
			continue
		}
		if statusVal, _ := m.Data["status"].(string); statusVal != teams.StatusAccepted {
			continue
		}
		if containsRole(membershipRoleLabels(m.Data["roles"]), teams.RoleOwner) {
			otherOwners++
		}
	}
	if otherOwners == 0 {
		return status.Error(codes.FailedPrecondition, "team must keep at least one owner")
	}
	return nil
}

func containsRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

func (t *Teams) resolveUserIDByEmail(ctx context.Context, projectID, email string) (string, error) {
	list, err := t.docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{
		Queries:  []string{query.BuildEqual("email", email)},
		PageSize: 1,
	}, databases.SystemPrincipal)
	if err != nil {
		return "", err
	}
	if len(list.Documents) == 0 {
		return "", nil
	}
	return list.Documents[0].ID, nil
}

func (t *Teams) adjustTeamTotal(ctx context.Context, projectID, teamID string, delta int, _ databases.Principal) error {
	doc, err := t.getTeamDoc(ctx, projectID, teamID, databases.SystemPrincipal)
	if err != nil {
		return err
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
	_, err = t.docDB.UpdateDocument(ctx, projectID, "default", "teams", databases.SimpleDocumentUpdate(databases.Document{
		ID:   teamID,
		Data: map[string]any{"total": total},
	}, nil), databases.SystemPrincipal)
	return err
}

func membershipPermissions(teamID, userID string) []databases.Permission {
	perms := []databases.Permission{
		{Type: "read", Role: fmt.Sprintf("team:%s", teamID)},
		{Type: "update", Role: fmt.Sprintf("team:%s", teamID)},
		{Type: "delete", Role: fmt.Sprintf("team:%s", teamID)},
		{Type: "read", Role: "keys"},
		{Type: "update", Role: "keys"},
		{Type: "delete", Role: "keys"},
		{Type: "read", Role: "admin"},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: "admin"},
	}
	if userID != "" {
		perms = append(perms,
			databases.Permission{Type: "read", Role: fmt.Sprintf("user:%s", userID)},
			databases.Permission{Type: "update", Role: fmt.Sprintf("user:%s", userID)},
			databases.Permission{Type: "delete", Role: fmt.Sprintf("user:%s", userID)},
		)
	}
	return perms
}

func defaultTeamPermissions(teamID string, explicit []string) []databases.Permission {
	if len(explicit) > 0 {
		var perms []databases.Permission
		for _, r := range explicit {
			parts := splitPermission(r)
			if len(parts) == 2 {
				perms = append(perms, databases.Permission{Type: parts[0], Role: parts[1]})
			}
		}
		return perms
	}
	return []databases.Permission{
		{Type: "read", Role: "any"},
		{Type: "read", Role: fmt.Sprintf("team:%s", teamID)},
		{Type: "update", Role: fmt.Sprintf("team:%s", teamID)},
		{Type: "delete", Role: fmt.Sprintf("team:%s", teamID)},
		{Type: "read", Role: "admin"},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: "admin"},
		{Type: "read", Role: "keys"},
		{Type: "update", Role: "keys"},
		{Type: "delete", Role: "keys"},
	}
}

func splitPermission(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return nil
}

func principalOrSystem(principal databases.Principal) databases.Principal {
	if len(principal.Roles) == 0 {
		return databases.SystemPrincipal
	}
	return principal
}
