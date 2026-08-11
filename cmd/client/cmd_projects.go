package main

import (
	"os"

	"github.com/spf13/cobra"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
)

// newProjectsCmd 提供 ProjectsService 的 list/get。
// CreateProject/UpdateProject 限平台 admin（console session），API Key 无法调用，CLI 不提供。
func newProjectsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "项目管理（list/get；create/update 限平台 admin，CLI 不提供）",
	}
	cmd.AddCommand(
		newProjectsListCmd(g),
		newProjectsGetCmd(g),
	)
	return cmd
}

func newProjectsListCmd(g *globalFlags) *cobra.Command {
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出项目",
		RunE: func(cmd *cobra.Command, args []string) error {
			req := buildListRequest(pageSize, pageToken)
			resp := &serverv1.ListProjectsResponse{}
			if err := invoke(g, serverv1.ProjectsService_ListProjects_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().Int32Var(&pageSize, "page-size", 0, "每页条数（服务端默认 50，上限 1000）")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "上一页返回的 next_page_token")
	return cmd
}

func newProjectsGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "按 ID 获取项目",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.Project{}
			if err := invoke(g, serverv1.ProjectsService_GetProject_FullMethodName, &serverv1.GetProjectRequest{Id: args[0]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// buildListRequest 由 --page-size/--page-token 构造共享的 ListRequest。
func buildListRequest(pageSize int32, pageToken string) *sharedv1.ListRequest {
	req := &sharedv1.ListRequest{PageToken: pageToken}
	if pageSize > 0 {
		req.PageSize = pageSize
	}
	return req
}
