package main

import (
	"os"

	"github.com/spf13/cobra"
)

// newRPCCmd 是逃生舱命令：按完整 gRPC 方法名调用任意 Server API 方法，
// --data 以 protojson 填充请求体（动态分发见 sdk/go/server.InvokeJSON，
// 完整性由 SDK 测试保证）。
func newRPCCmd(g *globalFlags) *cobra.Command {
	var data string
	cmd := &cobra.Command{
		Use:   "rpc <full-method> [--data '<json>']",
		Short: "通用调用：按完整 gRPC 方法名调用任意 Server API 方法（逃生舱）",
		Long: `按完整 gRPC 方法名调用 Server API 的任意 unary 方法（APIKeysService 除外——
API Key 凭证被服务端禁止调用）。--data 为请求的 protojson（camelCase 字段名，
可省略字段）。

示例：
  torchwood rpc /torchwood.server.v1.UsersService/ListUsers --data '{"pageSize": 10}'
  torchwood rpc /torchwood.server.v1.HealthService/Check
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, args[0], data)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&data, "data", "", "请求 JSON（protojson，camelCase 字段名）")
	return cmd
}
