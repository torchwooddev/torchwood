package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// newUsersCmd 覆盖 UsersService 全部 9 个方法：
// list/get/create/update/update-password/delete、sessions list/delete、tokens create。
// 标量参数用具名 flag，labels/prefs 等 Struct 字段用 --data 传入 JSON 合并。
func newUsersCmd(g *globalFlags) *cobra.Command {
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
			resp := &serverv1.ListUsersResponse{}
			if err := invoke(g, serverv1.UsersService_ListUsers_FullMethodName, buildListRequest(pageSize, pageToken), resp); err != nil {
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
			resp := &serverv1.User{}
			if err := invoke(g, serverv1.UsersService_GetUser_FullMethodName, &serverv1.GetUserRequest{Id: args[0]}, resp); err != nil {
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
			resp := &serverv1.User{}
			if err := invoke(g, serverv1.UsersService_CreateUser_FullMethodName, req, resp); err != nil {
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
			req, err := buildUpdateUserReq(args[0], changedBoolPtr(cmd, "email-verified", emailVerified), name, email, status, data)
			if err != nil {
				return err
			}
			resp := &serverv1.User{}
			if err := invoke(g, serverv1.UsersService_UpdateUser_FullMethodName, req, resp); err != nil {
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
			resp := &serverv1.User{}
			if err := invoke(g, serverv1.UsersService_UpdateUserPassword_FullMethodName, &serverv1.UpdateUserPasswordRequest{Id: args[0], Password: password}, resp); err != nil {
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
			resp := &serverv1.User{}
			if err := invoke(g, serverv1.UsersService_DeleteUser_FullMethodName, &serverv1.GetUserRequest{Id: args[0]}, resp); err != nil {
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
				resp := &serverv1.ListUserSessionsResponse{}
				if err := invoke(g, serverv1.UsersService_ListUserSessions_FullMethodName, &serverv1.GetUserRequest{Id: args[0]}, resp); err != nil {
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
				resp := &serverv1.User{}
				if err := invoke(g, serverv1.UsersService_DeleteUserSession_FullMethodName, &serverv1.DeleteUserSessionRequest{Id: args[0], SessionId: args[1]}, resp); err != nil {
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
				resp := &serverv1.CreateUserTokenResponse{}
				if err := invoke(g, serverv1.UsersService_CreateUserToken_FullMethodName, &serverv1.GetUserRequest{Id: args[0]}, resp); err != nil {
					return err
				}
				return printJSON(os.Stdout, resp)
			},
		},
	)
	return cmd
}

// buildCreateUserReq 由 flag 参数构造 CreateUserRequest；--data 以 protojson
// 合并（labels/prefs 等 Struct 字段），与 flag 冲突时以 --data 为准。
func buildCreateUserReq(email, password, name, status, data string) (*serverv1.CreateUserRequest, error) {
	if email == "" || password == "" {
		return nil, fmt.Errorf("--email 与 --password 必填")
	}
	req := &serverv1.CreateUserRequest{
		Email:    email,
		Password: password,
		Name:     name,
		Status:   status,
	}
	if err := mergeData(req, data); err != nil {
		return nil, err
	}
	return req, nil
}

// buildUpdateUserReq 构造 UpdateUserRequest：仅设置显式传入的字段，
// emailVerified 为 nil 表示未设置（proto3 optional presence）。
func buildUpdateUserReq(id string, emailVerified *bool, name, email, status, data string) (*serverv1.UpdateUserRequest, error) {
	if id == "" {
		return nil, fmt.Errorf("缺少用户 ID")
	}
	req := &serverv1.UpdateUserRequest{Id: id, EmailVerified: emailVerified}
	if name != "" {
		req.Name = name
	}
	if email != "" {
		req.Email = email
	}
	if status != "" {
		req.Status = status
	}
	if err := mergeData(req, data); err != nil {
		return nil, err
	}
	return req, nil
}

// mergeData 把 --data JSON 以 protojson 解析到同类型新消息后 proto.Merge 进请求
// （protojson.Unmarshal 会重置目标消息，不能直接复用）；--data 与 flag 冲突时以 --data 为准。
func mergeData(req proto.Message, data string) error {
	if data == "" {
		return nil
	}
	dataMsg := req.ProtoReflect().New().Interface()
	if err := protojson.Unmarshal([]byte(data), dataMsg); err != nil {
		return fmt.Errorf("--data 解析失败：%v", err)
	}
	proto.Merge(req, dataMsg)
	return nil
}
