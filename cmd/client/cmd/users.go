package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	methodUsersList           = "/torchwood.server.v1.UsersService/ListUsers"
	methodUsersGet            = "/torchwood.server.v1.UsersService/GetUser"
	methodUsersCreate         = "/torchwood.server.v1.UsersService/CreateUser"
	methodUsersUpdate         = "/torchwood.server.v1.UsersService/UpdateUser"
	methodUsersUpdatePassword = "/torchwood.server.v1.UsersService/UpdateUserPassword"
	methodUsersDelete         = "/torchwood.server.v1.UsersService/DeleteUser"
	methodUsersListSessions   = "/torchwood.server.v1.UsersService/ListUserSessions"
	methodUsersDeleteSession  = "/torchwood.server.v1.UsersService/DeleteUserSession"
	methodUsersCreateToken    = "/torchwood.server.v1.UsersService/CreateUserToken"
)

// newUsersCmd 覆盖 UsersService 全部 9 个方法：
// list/get/create/update/update-password/delete、sessions list/delete、tokens create。
// 标量参数用具名 flag，labels/prefs 等 Struct 字段用 --data 传入 JSON 合并。
func NewUsersCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "用户管理（UsersService 全部方法）",
	}
	cmd.AddCommand(
		newUsersListCmd(g),
		newUsersGetCmd(g),
		newUsersCreateCmd(g),
		newUsersUpdateCmd(g),
		newUsersUpdatePasswordCmd(g),
		newUsersDeleteCmd(g),
		newUsersSessionsCmd(g),
		newUsersTokensCmd(g),
	)
	return cmd
}

func newUsersListCmd(g *globalFlags) *cobra.Command {
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出用户",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodUsersList, listJSON(pageSize, pageToken))
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().Int32Var(&pageSize, "page-size", 0, "每页条数（服务端默认 50，上限 1000）")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "上一页返回的 next_page_token")
	return cmd
}

func newUsersGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "按 ID 获取用户",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodUsersGet, map[string]any{"id": args[0]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newUsersCreateCmd(g *globalFlags) *cobra.Command {
	var email, password, name, status, data string
	cmd := &cobra.Command{
		Use:   "create --email <e> --password <p>",
		Short: "创建用户",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateUserReq(email, password, name, status, data)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodUsersCreate, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "邮箱（必填）")
	cmd.Flags().StringVar(&password, "password", "", "密码（必填）")
	cmd.Flags().StringVar(&name, "name", "", "姓名")
	cmd.Flags().StringVar(&status, "status", "", "状态（如 active/inactive）")
	cmd.Flags().StringVar(&data, "data", "", "labels/prefs 等字段的 JSON（与 flag 冲突时以 --data 为准）")
	return cmd
}

func newUsersUpdateCmd(g *globalFlags) *cobra.Command {
	var emailVerified bool
	var name, email, status, data string
	cmd := &cobra.Command{
		Use:   "update <id> [--name] [--email] [--status] [--email-verified] [--data]",
		Short: "更新用户（仅更新显式传入的字段；清空字段请用 --data）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateUserReq(cmd, args[0], emailVerified, name, email, status, data)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodUsersUpdate, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().BoolVar(&emailVerified, "email-verified", false, "是否已验证邮箱（显式传 --email-verified=true/false 才生效）")
	cmd.Flags().StringVar(&name, "name", "", "姓名")
	cmd.Flags().StringVar(&email, "email", "", "邮箱")
	cmd.Flags().StringVar(&status, "status", "", "状态（如 active/inactive）")
	cmd.Flags().StringVar(&data, "data", "", "labels/prefs 等字段的 JSON（与 flag 冲突时以 --data 为准）")
	return cmd
}

func newUsersUpdatePasswordCmd(g *globalFlags) *cobra.Command {
	var password string
	cmd := &cobra.Command{
		Use:   "update-password <id> --password <p>",
		Short: "重置用户密码",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if password == "" {
				return fmt.Errorf("--password 必填")
			}
			resp, err := invoke(g, methodUsersUpdatePassword, map[string]any{"id": args[0], "password": password})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&password, "password", "", "新密码（必填）")
	return cmd
}

func newUsersDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "删除用户",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodUsersDelete, map[string]any{"id": args[0]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// newUsersSessionsCmd: users sessions list <id> / delete <id> <session-id>
func newUsersSessionsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "用户会话管理",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list <user-id>",
			Short: "列出用户会话",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				resp, err := invoke(g, methodUsersListSessions, map[string]any{"id": args[0]})
				if err != nil {
					return err
				}
				return printJSON(os.Stdout, resp)
			},
		},
		&cobra.Command{
			Use:   "delete <user-id> <session-id>",
			Short: "删除用户会话",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				resp, err := invoke(g, methodUsersDeleteSession, map[string]any{"id": args[0], "sessionId": args[1]})
				if err != nil {
					return err
				}
				return printJSON(os.Stdout, resp)
			},
		},
	)
	return cmd
}

// newUsersTokensCmd: users tokens create <id>
func newUsersTokensCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "用户令牌管理",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "create <user-id>",
			Short: "为用户创建访问/刷新令牌",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				resp, err := invoke(g, methodUsersCreateToken, map[string]any{"id": args[0]})
				if err != nil {
					return err
				}
				return printJSON(os.Stdout, resp)
			},
		},
	)
	return cmd
}

// buildCreateUserReq 由 flag 参数构造 CreateUserRequest JSON map；
// --data 以 JSON 覆盖合并（labels/prefs 等 Struct 字段），与 flag 冲突时以 --data 为准。
func buildCreateUserReq(email, password, name, status, data string) (map[string]any, error) {
	if email == "" || password == "" {
		return nil, fmt.Errorf("--email 与 --password 必填")
	}
	req := map[string]any{"email": email, "password": password}
	if name != "" {
		req["name"] = name
	}
	if status != "" {
		req["status"] = status
	}
	if err := mergeJSON(req, data); err != nil {
		return nil, err
	}
	return req, nil
}

// buildUpdateUserReq 构造 UpdateUserRequest JSON map：仅设置显式传入的字段，
// emailVerified 依赖 flag presence（proto3 optional 语义用键存在性表达）。
func buildUpdateUserReq(cmd *cobra.Command, id string, emailVerified bool, name, email, status, data string) (map[string]any, error) {
	if id == "" {
		return nil, fmt.Errorf("缺少用户 ID")
	}
	req := map[string]any{"id": id}
	setChanged(cmd, "email-verified", req, "emailVerified", emailVerified)
	if name != "" {
		req["name"] = name
	}
	if email != "" {
		req["email"] = email
	}
	if status != "" {
		req["status"] = status
	}
	if err := mergeJSON(req, data); err != nil {
		return nil, err
	}
	return req, nil
}
