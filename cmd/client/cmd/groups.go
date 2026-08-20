package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	methodGroupsCreate                 = "/torchwood.server.v1.GroupsService/CreateGroup"
	methodGroupsList                   = "/torchwood.server.v1.GroupsService/ListGroups"
	methodGroupsGet                    = "/torchwood.server.v1.GroupsService/GetGroup"
	methodGroupsDelete                 = "/torchwood.server.v1.GroupsService/DeleteGroup"
	methodGroupsGetPrefs               = "/torchwood.server.v1.GroupsService/GetGroupPrefs"
	methodGroupsUpdatePrefs            = "/torchwood.server.v1.GroupsService/UpdateGroupPrefs"
	methodGroupsCreateMembership       = "/torchwood.server.v1.GroupsService/CreateMembership"
	methodGroupsListMemberships        = "/torchwood.server.v1.GroupsService/ListMemberships"
	methodGroupsGetMembership          = "/torchwood.server.v1.GroupsService/GetMembership"
	methodGroupsUpdateMembership       = "/torchwood.server.v1.GroupsService/UpdateMembership"
	methodGroupsUpdateMembershipStatus = "/torchwood.server.v1.GroupsService/UpdateMembershipStatus"
	methodGroupsDeleteMembership       = "/torchwood.server.v1.GroupsService/DeleteMembership"
)

// NewGroupsCmd 覆盖 GroupsService 全部 12 个方法：
// 用户组（create/list/get/delete）、prefs（get/update）、
// memberships（create/list/get/update/update-status/delete）。
func NewGroupsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "用户组管理（GroupsService 全部方法）",
	}
	cmd.AddCommand(
		newGroupsCreateCmd(g),
		newGroupsListCmd(g),
		newGroupsGetCmd(g),
		newGroupsDeleteCmd(g),
		newGroupsPrefsCmd(g),
		newGroupsMembershipsCmd(g),
	)
	return cmd
}

func newGroupsCreateCmd(g *globalFlags) *cobra.Command {
	var name, permissions string
	cmd := &cobra.Command{
		Use:   "create --name <name>",
		Short: "创建用户组",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateGroupReq(name, permissions)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodGroupsCreate, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "用户组名称（必填）")
	cmd.Flags().StringVar(&permissions, "permissions", "", "权限 JSON 数组")
	return cmd
}

func newGroupsListCmd(g *globalFlags) *cobra.Command {
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出用户组",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodGroupsList, listJSON(pageSize, pageToken))
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

func newGroupsGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "按 ID 获取用户组",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodGroupsGet, map[string]any{"id": args[0]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newGroupsDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "删除用户组",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodGroupsDelete, map[string]any{"id": args[0]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// newGroupsPrefsCmd: groups prefs get <id> / update <id> --data。
func newGroupsPrefsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prefs",
		Short: "用户组偏好管理",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get <id>",
			Short: "获取用户组偏好",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				resp, err := invoke(g, methodGroupsGetPrefs, map[string]any{"id": args[0]})
				if err != nil {
					return err
				}
				return printJSON(os.Stdout, resp)
			},
		},
		newGroupsPrefsUpdateCmd(g),
	)
	return cmd
}

func newGroupsPrefsUpdateCmd(g *globalFlags) *cobra.Command {
	var data string
	cmd := &cobra.Command{
		Use:   "update <id> --data '{...}'",
		Short: "全量替换用户组偏好（--data 为 prefs 对象本身）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateGroupPrefsReq(args[0], data)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodGroupsUpdatePrefs, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&data, "data", "", "prefs JSON 对象（必填，如 '{\"theme\":\"dark\"}'）")
	return cmd
}

// newGroupsMembershipsCmd: groups memberships create/list/get/update/
// update-status/delete。
func newGroupsMembershipsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memberships",
		Short: "用户组成员管理",
	}
	cmd.AddCommand(
		newGroupsMembershipsCreateCmd(g),
		newGroupsMembershipsListCmd(g),
		newGroupsMembershipsGetCmd(g),
		newGroupsMembershipsUpdateCmd(g),
		newGroupsMembershipsUpdateStatusCmd(g),
		newGroupsMembershipsDeleteCmd(g),
	)
	return cmd
}

func newGroupsMembershipsCreateCmd(g *globalFlags) *cobra.Command {
	var userID, email, name, roles, status string
	cmd := &cobra.Command{
		Use:   "create <group-id> [--user-id <uid> | --email <email>]",
		Short: "创建用户组成员（--user-id 或 --email 至少一个）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateMembershipReq(args[0], userID, email, name, roles, status)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodGroupsCreateMembership, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&userID, "user-id", "", "用户 ID（已注册用户）")
	cmd.Flags().StringVar(&email, "email", "", "邮箱（邀请未注册用户）")
	cmd.Flags().StringVar(&name, "name", "", "成员姓名（邮箱邀请时使用）")
	cmd.Flags().StringVar(&roles, "roles", "", "角色 JSON 数组（如 '[\"admin\"]'）")
	cmd.Flags().StringVar(&status, "status", "", "状态（pending/active/blocked）")
	return cmd
}

func newGroupsMembershipsListCmd(g *globalFlags) *cobra.Command {
	var queries string
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list <group-id>",
		Short: "列出用户组成员",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildListMembershipsReq(args[0], queries, pageSize, pageToken)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodGroupsListMemberships, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&queries, "queries", "", "Appwrite 风格查询 JSON 数组")
	cmd.Flags().Int32Var(&pageSize, "page-size", 0, "每页条数（服务端默认 50，上限 1000）")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "上一页返回的 next_page_token")
	return cmd
}

func newGroupsMembershipsGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <group-id> <membership-id>",
		Short: "按 ID 获取用户组成员",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodGroupsGetMembership, map[string]any{"groupId": args[0], "membershipId": args[1]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newGroupsMembershipsUpdateCmd(g *globalFlags) *cobra.Command {
	var roles string
	cmd := &cobra.Command{
		Use:   "update <group-id> <membership-id> --roles '[...]'",
		Short: "全量替换成员角色",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateMembershipReq(args[0], args[1], roles)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodGroupsUpdateMembership, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&roles, "roles", "", "角色 JSON 数组（必填，全量替换）")
	return cmd
}

func newGroupsMembershipsUpdateStatusCmd(g *globalFlags) *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "update-status <group-id> <membership-id> --status <status>",
		Short: "更新成员状态（active/blocked；pending 不可回退）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateMembershipStatusReq(args[0], args[1], status)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodGroupsUpdateMembershipStatus, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "目标状态（必填：active/blocked）")
	return cmd
}

func newGroupsMembershipsDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <group-id> <membership-id>",
		Short: "删除用户组成员",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodGroupsDeleteMembership, map[string]any{"groupId": args[0], "membershipId": args[1]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// buildCreateGroupReq 构造 CreateGroupRequest（name 必填）。
func buildCreateGroupReq(name, permissions string) (map[string]any, error) {
	if name == "" {
		return nil, fmt.Errorf("--name 必填")
	}
	req := map[string]any{"name": name}
	if permissions != "" {
		perms, err := jsonStringList(permissions, "--permissions")
		if err != nil {
			return nil, err
		}
		req["permissions"] = perms
	}
	return req, nil
}

// buildUpdateGroupPrefsReq 构造 UpdateGroupPrefsRequest（--data 为 prefs 对象本体）。
func buildUpdateGroupPrefsReq(id, data string) (map[string]any, error) {
	if id == "" {
		return nil, fmt.Errorf("缺少用户组 ID")
	}
	req := map[string]any{"id": id}
	prefs, err := jsonObject(data, "--data")
	if err != nil {
		return nil, err
	}
	if prefs == nil {
		return nil, fmt.Errorf("--data 必填（prefs JSON 对象）")
	}
	req["prefs"] = prefs
	return req, nil
}

// buildCreateMembershipReq 构造 CreateMembershipRequest（user-id/email 至少一个）。
func buildCreateMembershipReq(groupID, userID, email, name, roles, status string) (map[string]any, error) {
	if groupID == "" {
		return nil, fmt.Errorf("缺少 group-id")
	}
	if userID == "" && email == "" {
		return nil, fmt.Errorf("--user-id 与 --email 至少提供一个")
	}
	req := map[string]any{"groupId": groupID}
	if userID != "" {
		req["userId"] = userID
	}
	if email != "" {
		req["email"] = email
	}
	if name != "" {
		req["name"] = name
	}
	if roles != "" {
		roleList, err := jsonStringList(roles, "--roles")
		if err != nil {
			return nil, err
		}
		req["roles"] = roleList
	}
	if status != "" {
		req["status"] = status
	}
	return req, nil
}

// buildListMembershipsReq 构造 ListMembershipsRequest。
func buildListMembershipsReq(groupID, queries string, pageSize int32, pageToken string) (map[string]any, error) {
	if groupID == "" {
		return nil, fmt.Errorf("缺少 group-id")
	}
	req := map[string]any{"groupId": groupID}
	if queries != "" {
		qs, err := jsonStringList(queries, "--queries")
		if err != nil {
			return nil, err
		}
		req["queries"] = qs
	}
	if pageSize > 0 {
		req["pageSize"] = pageSize
	}
	if pageToken != "" {
		req["pageToken"] = pageToken
	}
	return req, nil
}

// buildUpdateMembershipReq 构造 UpdateMembershipRequest（roles 必填）。
func buildUpdateMembershipReq(groupID, membershipID, roles string) (map[string]any, error) {
	if groupID == "" {
		return nil, fmt.Errorf("缺少 group-id")
	}
	if membershipID == "" {
		return nil, fmt.Errorf("缺少 membership-id")
	}
	roleList, err := jsonStringList(roles, "--roles")
	if err != nil {
		return nil, err
	}
	if len(roleList) == 0 {
		return nil, fmt.Errorf("--roles 必填")
	}
	return map[string]any{"groupId": groupID, "membershipId": membershipID, "roles": roleList}, nil
}

// buildUpdateMembershipStatusReq 构造 UpdateMembershipStatusRequest（status 必填）。
func buildUpdateMembershipStatusReq(groupID, membershipID, status string) (map[string]any, error) {
	if groupID == "" {
		return nil, fmt.Errorf("缺少 group-id")
	}
	if membershipID == "" {
		return nil, fmt.Errorf("缺少 membership-id")
	}
	if status == "" {
		return nil, fmt.Errorf("--status 必填")
	}
	return map[string]any{"groupId": groupID, "membershipId": membershipID, "status": status}, nil
}
