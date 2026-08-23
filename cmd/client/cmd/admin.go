package cmd

import "github.com/spf13/cobra"

// NewAdminCmd 提供平台运维管理命令（W-J）。
func NewAdminCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "平台运维管理（outbox 死信等）",
	}
	cmd.AddCommand(NewOutboxCmd(g))
	return cmd
}
