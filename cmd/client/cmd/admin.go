package cmd

import "github.com/spf13/cobra"

// NewAdminCmd 提供平台运维管理命令（W-J）。
func NewAdminCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "平台运维管理（outbox 死信、项目 export/import 等）",
	}
	cmd.AddCommand(NewOutboxCmd(g))
	// B5 export/import 直连数据库（不经 InvokeJSON/API 面）：导出需要
	// tw_system 旁路身份与 catalog/outbox 直读，POC 运维工具属性允许直连；
	// 不接 globalFlags（无 server 地址依赖），DSN 走 --dsn/环境变量。
	cmd.AddCommand(newAdminExportCmd(), newAdminImportCmd())
	return cmd
}
