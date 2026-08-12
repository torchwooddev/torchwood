package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	methodTeamsCreate                 = "/torchwood.server.v1.TeamsService/CreateTeam"
	methodTeamsList                   = "/torchwood.server.v1.TeamsService/ListTeams"
	methodTeamsGet                    = "/torchwood.server.v1.TeamsService/GetTeam"
	methodTeamsDelete                 = "/torchwood.server.v1.TeamsService/DeleteTeam"
	methodTeamsGetPrefs               = "/torchwood.server.v1.TeamsService/GetTeamPrefs"
	methodTeamsUpdatePrefs            = "/torchwood.server.v1.TeamsService/UpdateTeamPrefs"
	methodTeamsCreateMembership       = "/torchwood.server.v1.TeamsService/CreateMembership"
	methodTeamsListMemberships        = "/torchwood.server.v1.TeamsService/ListMemberships"
	methodTeamsGetMembership          = "/torchwood.server.v1.TeamsService/GetMembership"
	methodTeamsUpdateMembership       = "/torchwood.server.v1.TeamsService/UpdateMembership"
	methodTeamsUpdateMembershipStatus = "/torchwood.server.v1.TeamsService/UpdateMembershipStatus"
	methodTeamsDeleteMembership       = "/torchwood.server.v1.TeamsService/DeleteMembership"
)

// newTeamsCmd 覆盖 TeamsService 全部 12 个方法：
// 团队（create/list/get/delete）、prefs（get/update）、
// memberships（create/list/get/update/update-status/delete）。
func NewTeamsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "teams",
		Short: "团队管理（TeamsService 全部方法）",
	}
	cmd.AddCommand(
		newTeamsCreateCmd(g),
		newTeamsListCmd(g),
		newTeamsGetCmd(g),
		newTeamsDeleteCmd(g),
		newTeamsPrefsCmd(g),
		newTeamsMembershipsCmd(g),
	)
	return cmd
}

func newTeamsCreateCmd(g *globalFlags) *cobra.Command {
	var name, permissions string
	cmd := &cobra.Command{
		Use:   "create --name <name>",
		Short: "创建团队",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateTeamReq(name, permissions)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodTeamsCreate, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "团队名称（必填）")
	cmd.Flags().StringVar(&permissions, "permissions", "", "权限 JSON 数组")
	return cmd
}

func newTeamsListCmd(g *globalFlags) *cobra.Command {
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出团队",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodTeamsList, listJSON(pageSize, pageToken))
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

func newTeamsGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "按 ID 获取团队",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodTeamsGet, map[string]any{"id": args[0]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newTeamsDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "删除团队",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodTeamsDelete, map[string]any{"id": args[0]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// newTeamsPrefsCmd: teams prefs get <id> / update <id> --data。
func newTeamsPrefsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prefs",
		Short: "团队偏好管理",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get <id>",
			Short: "获取团队偏好",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				resp, err := invoke(g, methodTeamsGetPrefs, map[string]any{"id": args[0]})
				if err != nil {
					return err
				}
				return printJSON(os.Stdout, resp)
			},
		},
		newTeamsPrefsUpdateCmd(g),
	)
	return cmd
}

func newTeamsPrefsUpdateCmd(g *globalFlags) *cobra.Command {
	var data string
	cmd := &cobra.Command{
		Use:   "update <id> --data '{...}'",
		Short: "全量替换团队偏好（--data 为 prefs 对象本身）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateTeamPrefsReq(args[0], data)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodTeamsUpdatePrefs, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&data, "data", "", "prefs JSON 对象（必填，如 '{\"theme\":\"dark\"}'）")
	return cmd
}

// newTeamsMembershipsCmd: teams memberships create/list/get/update/
// update-status/delete。
func newTeamsMembershipsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memberships",
		Short: "团队成员管理",
	}
	cmd.AddCommand(
		newTeamsMembershipsCreateCmd(g),
		newTeamsMembershipsListCmd(g),
		newTeamsMembershipsGetCmd(g),
		newTeamsMembershipsUpdateCmd(g),
		newTeamsMembershipsUpdateStatusCmd(g),
		newTeamsMembershipsDeleteCmd(g),
	)
	return cmd
}

func newTeamsMembershipsCreateCmd(g *globalFlags) *cobra.Command {
	var userID, email, name, roles, status string
	cmd := &cobra.Command{
		Use:   "create <team-id> [--user-id <uid> | --email <email>]",
		Short: "创建团队成员（--user-id 或 --email 至少一个）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateMembershipReq(args[0], userID, email, name, roles, status)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodTeamsCreateMembership, req)
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

func newTeamsMembershipsListCmd(g *globalFlags) *cobra.Command {
	var queries string
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list <team-id>",
		Short: "列出团队成员",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildListMembershipsReq(args[0], queries, pageSize, pageToken)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodTeamsListMemberships, req)
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

func newTeamsMembershipsGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <team-id> <membership-id>",
		Short: "按 ID 获取团队成员",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodTeamsGetMembership, map[string]any{"teamId": args[0], "membershipId": args[1]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newTeamsMembershipsUpdateCmd(g *globalFlags) *cobra.Command {
	var roles string
	cmd := &cobra.Command{
		Use:   "update <team-id> <membership-id> --roles '[...]'",
		Short: "全量替换成员角色",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateMembershipReq(args[0], args[1], roles)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodTeamsUpdateMembership, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&roles, "roles", "", "角色 JSON 数组（必填，全量替换）")
	return cmd
}

func newTeamsMembershipsUpdateStatusCmd(g *globalFlags) *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "update-status <team-id> <membership-id> --status <status>",
		Short: "更新成员状态（active/blocked；pending 不可回退）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateMembershipStatusReq(args[0], args[1], status)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodTeamsUpdateMembershipStatus, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "目标状态（必填：active/blocked）")
	return cmd
}

func newTeamsMembershipsDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <team-id> <membership-id>",
		Short: "删除团队成员",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodTeamsDeleteMembership, map[string]any{"teamId": args[0], "membershipId": args[1]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// buildCreateTeamReq 构造 CreateTeamRequest（name 必填）。
func buildCreateTeamReq(name, permissions string) (map[string]any, error) {
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

// buildUpdateTeamPrefsReq 构造 UpdateTeamPrefsRequest（--data 为 prefs 对象本体）。
func buildUpdateTeamPrefsReq(id, data string) (map[string]any, error) {
	if id == "" {
		return nil, fmt.Errorf("缺少团队 ID")
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
func buildCreateMembershipReq(teamID, userID, email, name, roles, status string) (map[string]any, error) {
	if teamID == "" {
		return nil, fmt.Errorf("缺少 team-id")
	}
	if userID == "" && email == "" {
		return nil, fmt.Errorf("--user-id 与 --email 至少提供一个")
	}
	req := map[string]any{"teamId": teamID}
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
func buildListMembershipsReq(teamID, queries string, pageSize int32, pageToken string) (map[string]any, error) {
	if teamID == "" {
		return nil, fmt.Errorf("缺少 team-id")
	}
	req := map[string]any{"teamId": teamID}
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
func buildUpdateMembershipReq(teamID, membershipID, roles string) (map[string]any, error) {
	if teamID == "" {
		return nil, fmt.Errorf("缺少 team-id")
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
	return map[string]any{"teamId": teamID, "membershipId": membershipID, "roles": roleList}, nil
}

// buildUpdateMembershipStatusReq 构造 UpdateMembershipStatusRequest（status 必填）。
func buildUpdateMembershipStatusReq(teamID, membershipID, status string) (map[string]any, error) {
	if teamID == "" {
		return nil, fmt.Errorf("缺少 team-id")
	}
	if membershipID == "" {
		return nil, fmt.Errorf("缺少 membership-id")
	}
	if status == "" {
		return nil, fmt.Errorf("--status 必填")
	}
	return map[string]any{"teamId": teamID, "membershipId": membershipID, "status": status}, nil
}
