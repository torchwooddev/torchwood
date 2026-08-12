package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildCreateTeamReq(t *testing.T) {
	tests := []struct {
		name        string
		teamName    string
		permissions string
		wantErr     string
	}{
		{name: "缺 name", wantErr: "--name 必填"},
		{name: "最小字段", teamName: "核心组", wantErr: ""},
		{name: "带权限", teamName: "核心组", permissions: `["read(\"teams\")"]`, wantErr: ""},
		{name: "permissions 非法", teamName: "核心组", permissions: `oops`, wantErr: "--permissions 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateTeamReq(tt.teamName, tt.permissions)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.teamName, req["name"])
			if tt.permissions != "" {
				require.Equal(t, []string{`read("teams")`}, req["permissions"])
			} else {
				require.NotContains(t, req, "permissions")
			}
		})
	}
}

func TestBuildUpdateTeamPrefsReq(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		data    string
		wantErr string
	}{
		{name: "缺 id", wantErr: "缺少团队 ID"},
		{name: "缺 data", id: "t1", wantErr: "--data 必填"},
		{name: "data 非对象", id: "t1", data: `"str"`, wantErr: "--data 解析失败"},
		{name: "正常", id: "t1", data: `{"theme":"dark"}`, wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildUpdateTeamPrefsReq(tt.id, tt.data)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.id, req["id"])
			prefs, ok := req["prefs"].(map[string]any)
			require.True(t, ok, "prefs 不是 map: %v", req["prefs"])
			require.Equal(t, "dark", prefs["theme"])
		})
	}
}

func TestBuildCreateMembershipReq(t *testing.T) {
	tests := []struct {
		name       string
		teamID     string
		userID     string
		email      string
		memberName string
		roles      string
		status     string
		wantErr    string
	}{
		{name: "缺 team-id", wantErr: "缺少 team-id"},
		{name: "user-id/email 全缺", teamID: "t1", wantErr: "--user-id 与 --email 至少提供一个"},
		{name: "按 user-id", teamID: "t1", userID: "u1", roles: `["admin"]`, status: "active", wantErr: ""},
		{name: "按 email 邀请", teamID: "t1", email: "a@b.c", memberName: "Alice", wantErr: ""},
		{name: "roles 非法", teamID: "t1", userID: "u1", roles: `x`, wantErr: "--roles 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateMembershipReq(tt.teamID, tt.userID, tt.email, tt.memberName, tt.roles, tt.status)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.teamID, req["teamId"])
			if tt.userID != "" {
				require.Equal(t, tt.userID, req["userId"])
			} else {
				require.NotContains(t, req, "userId")
			}
			if tt.email != "" {
				require.Equal(t, tt.email, req["email"])
			} else {
				require.NotContains(t, req, "email")
			}
			if tt.memberName != "" {
				require.Equal(t, tt.memberName, req["name"])
			} else {
				require.NotContains(t, req, "name")
			}
			if tt.roles != "" {
				require.Equal(t, []string{"admin"}, req["roles"])
			} else {
				require.NotContains(t, req, "roles")
			}
			if tt.status != "" {
				require.Equal(t, tt.status, req["status"])
			} else {
				require.NotContains(t, req, "status")
			}
		})
	}
}

func TestBuildListMembershipsReq(t *testing.T) {
	tests := []struct {
		name      string
		teamID    string
		queries   string
		pageSize  int32
		pageToken string
		wantErr   string
	}{
		{name: "缺 team-id", wantErr: "缺少 team-id"},
		{name: "最小字段", teamID: "t1", wantErr: ""},
		{name: "queries 非法", teamID: "t1", queries: `x`, wantErr: "--queries 解析失败"},
		{name: "全字段", teamID: "t1", queries: `["equal(\"status\",\"active\")"]`, pageSize: 10, pageToken: "tok", wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildListMembershipsReq(tt.teamID, tt.queries, tt.pageSize, tt.pageToken)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.teamID, req["teamId"])
			if tt.queries != "" {
				require.Equal(t, []string{`equal("status","active")`}, req["queries"])
			} else {
				require.NotContains(t, req, "queries")
			}
			if tt.pageSize > 0 {
				require.Equal(t, int32(10), req["pageSize"])
			} else {
				require.NotContains(t, req, "pageSize")
			}
			if tt.pageToken != "" {
				require.Equal(t, tt.pageToken, req["pageToken"])
			} else {
				require.NotContains(t, req, "pageToken")
			}
		})
	}
}

func TestBuildUpdateMembershipReq(t *testing.T) {
	tests := []struct {
		name         string
		teamID       string
		membershipID string
		roles        string
		wantErr      string
	}{
		{name: "缺 membership-id", teamID: "t1", roles: `["admin"]`, wantErr: "缺少 membership-id"},
		{name: "缺 roles", teamID: "t1", membershipID: "m1", wantErr: "--roles 必填"},
		{name: "正常", teamID: "t1", membershipID: "m1", roles: `["admin","viewer"]`, wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildUpdateMembershipReq(tt.teamID, tt.membershipID, tt.roles)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.teamID, req["teamId"])
			require.Equal(t, tt.membershipID, req["membershipId"])
			require.Equal(t, []string{"admin", "viewer"}, req["roles"])
		})
	}
}

func TestBuildUpdateMembershipStatusReq(t *testing.T) {
	tests := []struct {
		name         string
		teamID       string
		membershipID string
		status       string
		wantErr      string
	}{
		{name: "缺 status", teamID: "t1", membershipID: "m1", wantErr: "--status 必填"},
		{name: "正常", teamID: "t1", membershipID: "m1", status: "blocked", wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildUpdateMembershipStatusReq(tt.teamID, tt.membershipID, tt.status)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.teamID, req["teamId"])
			require.Equal(t, tt.membershipID, req["membershipId"])
			require.Equal(t, tt.status, req["status"])
		})
	}
}
