package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// globalFlags 是贯穿全部子命令的全局参数（设计文档 §4.3）。
type globalFlags struct {
	endpoint   string // gRPC 地址（TORCHWOOD_CLI_ENDPOINT）
	apiKey     string // API Key secret（TORCHWOOD_CLI_API_KEY）
	timeout    string // 原始字符串，validate 校验后写入 timeoutDur
	timeoutDur time.Duration
	output     string // 输出格式（MVP 仅 json）
	tls        bool   // 占位：服务端当前为明文 gRPC，使用时报未支持
}

// envOr 返回环境变量值（非空时），否则回退到默认值。
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// annotationNoKey 标记命令无需 API key（health、uuid 等本地/公开命令）。
const annotationNoKey = "torchwood/no-key"

// cmdKeyExempt 判断命令是否豁免 API key 必填校验：health、uuid 等标注命令（含子命令）。
func cmdKeyExempt(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if _, ok := c.Annotations[annotationNoKey]; ok {
			return true
		}
	}
	return false
}

// NewRootCmd 构造根命令；version 由 main 包 ldflags 注入。
func NewRootCmd(version string) *cobra.Command {
	g := &globalFlags{}
	root := &cobra.Command{
		Use:   "torchwood",
		Short: "Torchwood CLI：通过 API Key 走 gRPC 调用 Server API",
		Long: `Torchwood CLI 通过 gRPC（非 HTTP gateway）调用 Server API，认证一律使用
x-api-key metadata（scope 见 API Key 的 scopes）。默认连接 127.0.0.1:9060
（服务端 gRPC 仅监听回环，远程使用需走 SSH 隧道或调整 server.grpc.addr）。`,
		Version:           version,
		SilenceErrors:     true,
		SilenceUsage:      true,
		PersistentPreRunE: g.validate,
	}
	root.PersistentFlags().StringVar(&g.endpoint, "endpoint", envOr("TORCHWOOD_CLI_ENDPOINT", "127.0.0.1:9060"), "gRPC 服务地址")
	root.PersistentFlags().StringVar(&g.apiKey, "api-key", envOr("TORCHWOOD_CLI_API_KEY", ""), "API Key secret（亦可用 TORCHWOOD_CLI_API_KEY 环境变量；health / uuid 除外必填）")
	root.PersistentFlags().StringVar(&g.timeout, "timeout", envOr("TORCHWOOD_CLI_TIMEOUT", "30s"), "单次调用超时（如 30s、1m）")
	root.PersistentFlags().StringVar(&g.output, "output", envOr("TORCHWOOD_CLI_OUTPUT", "json"), "输出格式（MVP 仅 json）")
	root.PersistentFlags().BoolVar(&g.tls, "tls", false, "使用 TLS（占位，暂未支持）")

	root.AddCommand(
		NewHealthCmd(g),
		NewUUIDCmd(),
		NewProjectsCmd(g),
		NewUsersCmd(g),
		NewDatabasesCmd(g),
		NewGroupsCmd(g),
		NewStorageCmd(g),
		NewFunctionsCmd(g),
		NewOAuthProvidersCmd(g),
		NewAdminCmd(g),
		NewRPCCmd(g),
	)
	return root
}

// validate 是 root 的 PersistentPreRunE：全局参数校验 + API key 必填检查。
func (g *globalFlags) validate(cmd *cobra.Command, _ []string) error {
	if g.output != "json" {
		return fmt.Errorf("不支持的输出格式 %q：MVP 仅支持 json", g.output)
	}
	d, err := time.ParseDuration(g.timeout)
	if err != nil {
		return fmt.Errorf("无效的 --timeout %q：%v", g.timeout, err)
	}
	g.timeoutDur = d
	if g.apiKey == "" && !cmdKeyExempt(cmd) {
		return fmt.Errorf("缺少 API key：请通过 --api-key 或 TORCHWOOD_CLI_API_KEY 提供（health / uuid 除外）")
	}
	return nil
}
