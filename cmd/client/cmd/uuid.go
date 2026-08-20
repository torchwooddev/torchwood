package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/torchwooddev/torchwood/pkg/idgen"
)

// NewUUIDCmd 生成本地 UUID v4（与服务端 idgen.UUID 同源），无需 API key。
// 输出纯文本 ID（一行一个），便于 shell 捕获后传给 --id 等客户端指定 ID 的命令。
func NewUUIDCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "uuid",
		Short:       "生成本地 UUID（无需 API key）",
		Long:        "使用与服务端相同的 idgen.UUID() 生成 UUID v4，便于为 databases/functions 等命令的 --id 提供客户端指定 ID。",
		Annotations: map[string]string{annotationNoKey: "true"},
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(os.Stdout, idgen.UUID().String())
			return err
		},
	}
}
