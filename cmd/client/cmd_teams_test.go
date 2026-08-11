package main

import (
	"strings"
	"testing"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"google.golang.org/protobuf/proto"
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
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetName() != tt.teamName {
				t.Errorf("name 不匹配: %q", req.GetName())
			}
			if tt.permissions != "" && len(req.GetPermissions()) != 1 {
				t.Errorf("permissions 未合并: %v", req.GetPermissions())
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
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetPrefs() == nil || req.GetPrefs().AsMap()["theme"] != "dark" {
				t.Errorf("prefs 未解析: %v", req.GetPrefs())
			}
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
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetTeamId() != tt.teamID || req.GetUserId() != tt.userID || req.GetEmail() != tt.email {
				t.Errorf("请求不匹配: %v", req)
			}
			if tt.roles != "" && len(req.GetRoles()) != 1 {
				t.Errorf("roles 未合并: %v", req.GetRoles())
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
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(req.GetRoles()) != 2 {
				t.Errorf("roles 未解析: %v", req.GetRoles())
			}
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
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetStatus() != tt.status {
				t.Errorf("status 不匹配: %q", req.GetStatus())
			}
		})
	}
}

// TestTeamsRegistryTypes 校验具名命令构造的请求类型与 rpc 注册表一致。
func TestTeamsRegistryTypes(t *testing.T) {
	for method, sample := range map[string]proto.Message{
		serverv1.TeamsService_CreateTeam_FullMethodName:             &serverv1.CreateTeamRequest{},
		serverv1.TeamsService_UpdateTeamPrefs_FullMethodName:        &serverv1.UpdateTeamPrefsRequest{},
		serverv1.TeamsService_CreateMembership_FullMethodName:       &serverv1.CreateMembershipRequest{},
		serverv1.TeamsService_UpdateMembership_FullMethodName:       &serverv1.UpdateMembershipRequest{},
		serverv1.TeamsService_UpdateMembershipStatus_FullMethodName: &serverv1.UpdateMembershipStatusRequest{},
	} {
		e, err := lookupRPCMethod(method)
		if err != nil {
			t.Fatal(err)
		}
		if proto.MessageName(e.newReq()) != proto.MessageName(sample) {
			t.Errorf("注册表请求类型与具名命令不一致: %s", method)
		}
	}
}
