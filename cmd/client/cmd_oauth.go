package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
)

// newOAuthProvidersCmd 覆盖 OAuthProvidersService 全部 3 个方法：
// list/upsert/delete（proto 无 get 方法；upsert 即 create+update 语义）。
func newOAuthProvidersCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oauth-providers",
		Short: "OAuth 提供商管理（OAuthProvidersService 全部方法）",
	}
	cmd.AddCommand(
		newOAuthProvidersListCmd(g),
		newOAuthProvidersUpsertCmd(g),
		newOAuthProvidersDeleteCmd(g),
	)
	return cmd
}

func newOAuthProvidersListCmd(g *globalFlags) *cobra.Command {
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出 OAuth 提供商",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.ListOAuthProvidersResponse{}
			if err := invoke(g, serverv1.OAuthProvidersService_ListOAuthProviders_FullMethodName, buildListRequest(pageSize, pageToken), resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().Int32Var(&pageSize, "page-size", 0, "每页条数（服务端默认 50，上限 1000）")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "上一页返回的 next_page_token")
	return cmd
}

func newOAuthProvidersUpsertCmd(g *globalFlags) *cobra.Command {
	var enabled bool
	var clientID, clientSecret, scopes string
	cmd := &cobra.Command{
		Use:   "upsert <provider> --client-id <id>",
		Short: "创建或更新 OAuth 提供商（如 google/github）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpsertOAuthProviderReq(args[0], enabled, clientID, clientSecret, scopes)
			if err != nil {
				return err
			}
			resp := &serverv1.OAuthProvider{}
			if err := invoke(g, serverv1.OAuthProvidersService_UpsertOAuthProvider_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().BoolVar(&enabled, "enabled", false, "是否启用（启用时必填 --client-secret）")
	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth client ID（必填）")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "OAuth client secret（启用时必填）")
	cmd.Flags().StringVar(&scopes, "scopes", "", "请求 scope JSON 数组（如 '[\"email\",\"profile\"]'）")
	return cmd
}

func newOAuthProvidersDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <provider>",
		Short: "删除 OAuth 提供商",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.OAuthProvider{}
			if err := invoke(g, serverv1.OAuthProvidersService_DeleteOAuthProvider_FullMethodName, &serverv1.DeleteOAuthProviderRequest{Provider: args[0]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// buildUpsertOAuthProviderReq 构造 UpsertOAuthProviderRequest（client-id 必填；
// 启用时 client-secret 必填，与服务端校验一致）。
func buildUpsertOAuthProviderReq(provider string, enabled bool, clientID, clientSecret, scopes string) (*serverv1.UpsertOAuthProviderRequest, error) {
	if provider == "" {
		return nil, fmt.Errorf("缺少 provider")
	}
	if clientID == "" {
		return nil, fmt.Errorf("--client-id 必填")
	}
	if enabled && clientSecret == "" {
		return nil, fmt.Errorf("--enabled=true 时 --client-secret 必填")
	}
	scopeList, err := jsonStringList(scopes, "scopes")
	if err != nil {
		return nil, err
	}
	return &serverv1.UpsertOAuthProviderRequest{
		Provider:     provider,
		Enabled:      enabled,
		ClientId:     clientID,
		ClientSecret: clientSecret,
		Scopes:       scopeList,
	}, nil
}
