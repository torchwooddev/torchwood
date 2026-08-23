package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

const (
	methodOutboxListDead  = "/torchwood.server.v1.OutboxService/ListDeadLetters"
	methodOutboxReplay    = "/torchwood.server.v1.OutboxService/ReplayDeadLetter"
)

// NewOutboxCmd 提供 outbox 死信管理（admin）。
func NewOutboxCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "outbox",
		Short: "outbox 死信管理（list-dead/replay）",
	}
	cmd.AddCommand(
		newOutboxListDeadCmd(g),
		newOutboxReplayCmd(g),
	)
	return cmd
}

func newOutboxListDeadCmd(g *globalFlags) *cobra.Command {
	var projectID string
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list-dead",
		Short: "列出死信",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			if projectID != "" {
				payload["project_id"] = projectID
			}
			if pageSize != 0 {
				payload["page_size"] = pageSize
			}
			if pageToken != "" {
				payload["page_token"] = pageToken
			}
			resp, err := invoke(g, methodOutboxListDead, payload)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&projectID, "project-id", "", "项目 ID（必填，与 API key 或 --project 绑定一致）")
	cmd.Flags().Int32Var(&pageSize, "page-size", 0, "每页条数")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "上一页 next_page_token")
	return cmd
}

func newOutboxReplayCmd(g *globalFlags) *cobra.Command {
	var projectID string
	cmd := &cobra.Command{
		Use:   "replay <event-id>",
		Short: "重放单条死信",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{"event_id": args[0]}
			if projectID != "" {
				payload["project_id"] = projectID
			}
			resp, err := invoke(g, methodOutboxReplay, payload)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&projectID, "project-id", "", "项目 ID（必填）")
	return cmd
}
