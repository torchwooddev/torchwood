package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// NewHealthCmd 提供 HealthService 两个公开方法（ACCESS_PUBLIC，无需 API key）。
// 整组命令标记 annotationNoKey，root 的 validate 据此豁免 api-key 必填校验。
func NewHealthCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "health",
		Short:       "健康检查（公开接口，无需 API key）",
		Annotations: map[string]string{annotationNoKey: "true"},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get",
			Short: "查询服务健康状态",
			RunE: func(cmd *cobra.Command, args []string) error {
				resp, err := invoke(g, "/torchwood.server.v1.HealthService/Check", nil)
				if err != nil {
					return err
				}
				return printJSON(os.Stdout, resp)
			},
		},
		&cobra.Command{
			Use:   "version",
			Short: "查询服务端构建版本",
			RunE: func(cmd *cobra.Command, args []string) error {
				resp, err := invoke(g, "/torchwood.server.v1.HealthService/GetVersion", nil)
				if err != nil {
					return err
				}
				return printJSON(os.Stdout, resp)
			},
		},
	)
	return cmd
}
