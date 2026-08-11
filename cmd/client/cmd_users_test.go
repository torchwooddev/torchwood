package main

import (
	"strings"
	"testing"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"google.golang.org/protobuf/proto"
)

// boolPtr 返回指向 v 的指针，用于 optional bool 字段。
func boolPtr(v bool) *bool { return &v }

func TestBuildCreateUserReq(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
		nameArg  string
		status   string
		data     string
		wantErr  string
	}{
		{name: "必填校验", email: "", password: "", wantErr: "--email 与 --password 必填"},
		{name: "最小字段", email: "a@b.c", password: "pw", wantErr: ""},
		{name: "全字段", email: "a@b.c", password: "pw", nameArg: "Alice", status: "active", wantErr: ""},
		{name: "data 合并 labels", email: "a@b.c", password: "pw", data: `{"labels":{"team":"core"}}`, wantErr: ""},
		{name: "data 非法 JSON", email: "a@b.c", password: "pw", data: `{invalid`, wantErr: "--data 解析失败"},
		{name: "data 未知字段", email: "a@b.c", password: "pw", data: `{"bogus":1}`, wantErr: "--data 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateUserReq(tt.email, tt.password, tt.nameArg, tt.status, tt.data)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetEmail() != tt.email || req.GetPassword() != tt.password {
				t.Errorf("email/password 不匹配: %v", req)
			}
			if tt.nameArg != "" && req.GetName() != tt.nameArg {
				t.Errorf("name 不匹配: %q", req.GetName())
			}
			if tt.status != "" && req.GetStatus() != tt.status {
				t.Errorf("status 不匹配: %q", req.GetStatus())
			}
			if tt.data != "" && tt.wantErr == "" {
				if labels := req.GetLabels(); labels != nil && labels.AsMap()["team"] != "core" {
					t.Errorf("labels 未合并: %v", labels)
				}
			}
		})
	}
}

func TestBuildUpdateUserReq(t *testing.T) {
	tests := []struct {
		name         string
		id           string
		emailVerifed *bool
		nameArg      string
		email        string
		status       string
		data         string
		wantErr      string
	}{
		{name: "缺 id", id: "", wantErr: "缺少用户 ID"},
		{name: "仅 id", id: "u1", wantErr: ""},
		{name: "optional bool 设置", id: "u1", emailVerifed: boolPtr(true), wantErr: ""},
		{name: "optional bool 未设置", id: "u1", wantErr: ""},
		{name: "全字段", id: "u1", emailVerifed: boolPtr(false), nameArg: "Bob", email: "b@c.d", status: "inactive", wantErr: ""},
		{name: "data 合并 prefs", id: "u1", data: `{"prefs":{"theme":"dark"}}`, wantErr: ""},
		{name: "data 非法", id: "u1", data: `{`, wantErr: "--data 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildUpdateUserReq(tt.id, tt.emailVerifed, tt.nameArg, tt.email, tt.status, tt.data)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetId() != tt.id {
				t.Errorf("id 不匹配: %q", req.GetId())
			}
			if tt.emailVerifed == nil {
				if req.EmailVerified != nil {
					t.Errorf("EmailVerified 应为未设置: %v", req.EmailVerified)
				}
			} else if req.EmailVerified == nil || *req.EmailVerified != *tt.emailVerifed {
				t.Errorf("EmailVerified 不匹配: %v", req.EmailVerified)
			}
			if tt.nameArg == "" && req.GetName() != "" {
				t.Errorf("未传 --name 不应设置 name: %q", req.GetName())
			}
			if tt.nameArg != "" && req.GetName() != tt.nameArg {
				t.Errorf("name 不匹配: %q", req.GetName())
			}
			if tt.data == "" && req.Prefs != nil {
				t.Errorf("未传 --data 不应设置 prefs: %v", req.Prefs)
			}
		})
	}
}

func TestBuildListRequest(t *testing.T) {
	tests := []struct {
		name      string
		pageSize  int32
		pageToken string
		wantSize  int32
		wantToken string
	}{
		{name: "默认分页", pageSize: 0, wantSize: 0, wantToken: ""},
		{name: "带分页参数", pageSize: 10, pageToken: "tok", wantSize: 10, wantToken: "tok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildListRequest(tt.pageSize, tt.pageToken)
			if req.GetPageSize() != tt.wantSize || req.GetPageToken() != tt.wantToken {
				t.Errorf("ListRequest 不匹配: %v", req)
			}
		})
	}
}

// TestRegistryRequestTypes 校验注册表中的请求类型能承载具名命令构造的消息
// （rpc 逃生舱与具名命令共享注册表的类型一致性）。
func TestRegistryRequestTypes(t *testing.T) {
	created := &serverv1.CreateUserRequest{}
	e, err := lookupRPCMethod(serverv1.UsersService_CreateUser_FullMethodName)
	if err != nil {
		t.Fatal(err)
	}
	if proto.MessageName(e.newReq()) != proto.MessageName(created) {
		t.Fatalf("注册表请求类型与具名命令不一致: %v vs %v",
			proto.MessageName(e.newReq()), proto.MessageName(created))
	}
}
