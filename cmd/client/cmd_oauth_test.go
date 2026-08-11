package main

import (
	"strings"
	"testing"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"google.golang.org/protobuf/proto"
)

func TestBuildUpsertOAuthProviderReq(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		enabled      bool
		clientID     string
		clientSecret string
		scopes       string
		wantErr      string
	}{
		{name: "缺 provider", clientID: "cid", wantErr: "缺少 provider"},
		{name: "缺 client-id", provider: "google", wantErr: "--client-id 必填"},
		{name: "启用缺 secret", provider: "google", enabled: true, clientID: "cid",
			wantErr: "--enabled=true 时 --client-secret 必填"},
		{name: "禁用最小字段", provider: "google", clientID: "cid", wantErr: ""},
		{name: "启用全字段", provider: "google", enabled: true, clientID: "cid",
			clientSecret: "sec", scopes: `["email","profile"]`, wantErr: ""},
		{name: "scopes 非法", provider: "google", clientID: "cid", scopes: `x`,
			wantErr: "--scopes 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildUpsertOAuthProviderReq(tt.provider, tt.enabled, tt.clientID, tt.clientSecret, tt.scopes)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetProvider() != tt.provider || req.GetClientId() != tt.clientID ||
				req.GetEnabled() != tt.enabled || req.GetClientSecret() != tt.clientSecret {
				t.Errorf("请求不匹配: %v", req)
			}
			if tt.scopes != "" && len(req.GetScopes()) != 2 {
				t.Errorf("scopes 未解析: %v", req.GetScopes())
			}
		})
	}
}

// TestOAuthRegistryTypes 校验具名命令构造的请求类型与 rpc 注册表一致。
func TestOAuthRegistryTypes(t *testing.T) {
	e, err := lookupRPCMethod(serverv1.OAuthProvidersService_UpsertOAuthProvider_FullMethodName)
	if err != nil {
		t.Fatal(err)
	}
	if proto.MessageName(e.newReq()) != proto.MessageName(&serverv1.UpsertOAuthProviderRequest{}) {
		t.Fatal("注册表请求类型与具名命令不一致")
	}
}
