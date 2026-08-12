package cmd

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	methodFunctionsRuntimes         = "/torchwood.server.v1.FunctionsService/ListRuntimes"
	methodFunctionsSpecifications   = "/torchwood.server.v1.FunctionsService/ListSpecifications"
	methodFunctionsCreate           = "/torchwood.server.v1.FunctionsService/CreateFunction"
	methodFunctionsList             = "/torchwood.server.v1.FunctionsService/ListFunctions"
	methodFunctionsGet              = "/torchwood.server.v1.FunctionsService/GetFunction"
	methodFunctionsUpdate           = "/torchwood.server.v1.FunctionsService/UpdateFunction"
	methodFunctionsDelete           = "/torchwood.server.v1.FunctionsService/DeleteFunction"
	methodFunctionsCreateDeployment = "/torchwood.server.v1.FunctionsService/CreateDeployment"
	methodFunctionsListDeployments  = "/torchwood.server.v1.FunctionsService/ListDeployments"
	methodFunctionsGetDeployment    = "/torchwood.server.v1.FunctionsService/GetDeployment"
	methodFunctionsDeleteDeployment = "/torchwood.server.v1.FunctionsService/DeleteDeployment"
	methodFunctionsSetVariables     = "/torchwood.server.v1.FunctionsService/SetVariables"
	methodFunctionsGetVariables     = "/torchwood.server.v1.FunctionsService/GetVariables"
	methodFunctionsCreateExecution  = "/torchwood.server.v1.FunctionsService/CreateExecution"
	methodFunctionsListExecutions   = "/torchwood.server.v1.FunctionsService/ListExecutions"
	methodFunctionsGetExecution     = "/torchwood.server.v1.FunctionsService/GetExecution"
)

// newFunctionsCmd 覆盖 FunctionsService 全部 16 个方法：
// runtimes/specifications、functions（create/list/get/update/delete）、
// deployments（create/list/get/delete）、variables（set/get）、
// executions（create/list/get）。
// deployments create 由 CLI 读取 zip 文件并 base64 编码后走 gRPC 纯消息
// （bytes code，≤1MiB 建议；gRPC 通道上限 8MiB，与服务端 MaxRecvMsgSize
// 对齐），更大的代码包走 multipart 上传（独立 HTTP handler，CLI 不提供）。
func NewFunctionsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "functions",
		Short: "函数管理（FunctionsService 全部方法）",
	}
	cmd.AddCommand(
		newFunctionsRuntimesCmd(g),
		newFunctionsSpecificationsCmd(g),
		newFunctionsListCmd(g),
		newFunctionsCreateCmd(g),
		newFunctionsGetCmd(g),
		newFunctionsUpdateCmd(g),
		newFunctionsDeleteCmd(g),
		newFunctionsDeploymentsCmd(g),
		newFunctionsVariablesCmd(g),
		newFunctionsExecutionsCmd(g),
	)
	return cmd
}

func newFunctionsRuntimesCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "runtimes",
		Short: "列出支持的运行时",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodFunctionsRuntimes, nil)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newFunctionsSpecificationsCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "specifications",
		Short: "列出支持的资源配置（spec）",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodFunctionsSpecifications, nil)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newFunctionsListCmd(g *globalFlags) *cobra.Command {
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出函数",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodFunctionsList, listJSON(pageSize, pageToken))
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

func newFunctionsCreateCmd(g *globalFlags) *cobra.Command {
	var id, name, runtime, entrypoint string
	var timeoutSeconds int32
	var spec string
	var enabled bool
	cmd := &cobra.Command{
		Use:   "create --id <id> --name <name> --runtime <runtime>",
		Short: "创建函数",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateFunctionReq(cmd, id, name, runtime, entrypoint, timeoutSeconds, spec, enabled)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodFunctionsCreate, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "函数 ID（必填）")
	cmd.Flags().StringVar(&name, "name", "", "函数名称（必填）")
	cmd.Flags().StringVar(&runtime, "runtime", "", "运行时（必填，见 runtimes 命令）")
	cmd.Flags().StringVar(&entrypoint, "entrypoint", "", "入口文件（缺省按运行时取默认值）")
	cmd.Flags().Int32Var(&timeoutSeconds, "timeout-seconds", 0, "超时秒数（1-300，缺省服务端默认）")
	cmd.Flags().StringVar(&spec, "spec", "", "资源配置（缺省 shared-1x，见 specifications 命令）")
	cmd.Flags().BoolVar(&enabled, "enabled", false, "是否启用（显式传 --enabled=true/false 才生效）")
	return cmd
}

func newFunctionsGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <function-id>",
		Short: "按 ID 获取函数",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodFunctionsGet, map[string]any{"functionId": args[0]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newFunctionsUpdateCmd(g *globalFlags) *cobra.Command {
	var name, entrypoint, spec string
	var timeoutSeconds int32
	var enabled bool
	cmd := &cobra.Command{
		Use:   "update <function-id> [--name] [--entrypoint] [--timeout-seconds] [--spec] [--enabled]",
		Short: "更新函数（仅更新显式传入的字段）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateFunctionReq(cmd, args[0], name, entrypoint, timeoutSeconds, spec, enabled)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodFunctionsUpdate, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "函数名称")
	cmd.Flags().StringVar(&entrypoint, "entrypoint", "", "入口文件")
	cmd.Flags().Int32Var(&timeoutSeconds, "timeout-seconds", 0, "超时秒数（1-300）")
	cmd.Flags().StringVar(&spec, "spec", "", "资源配置")
	cmd.Flags().BoolVar(&enabled, "enabled", false, "是否启用（显式传 --enabled=true/false 才生效）")
	return cmd
}

func newFunctionsDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <function-id>",
		Short: "删除函数",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodFunctionsDelete, map[string]any{"functionId": args[0]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// newFunctionsDeploymentsCmd: functions deployments create/list/get/delete。
func newFunctionsDeploymentsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployments",
		Short: "函数部署管理（code 为 zip 代码包）",
	}
	cmd.AddCommand(
		newFunctionsDeploymentsCreateCmd(g),
		newFunctionsDeploymentsListCmd(g),
		newFunctionsDeploymentsGetCmd(g),
		newFunctionsDeploymentsDeleteCmd(g),
	)
	return cmd
}

func newFunctionsDeploymentsCreateCmd(g *globalFlags) *cobra.Command {
	var code string
	cmd := &cobra.Command{
		Use:   "create <function-id> --code <zip-file>",
		Short: "上传 zip 代码包创建部署（gRPC 纯消息通道，上限 8MiB）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateDeploymentReq(args[0], code)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodFunctionsCreateDeployment, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "zip 代码包路径（必填；gRPC 消息通道上限 8MiB，建议单包 ≤1MiB；更大的代码包请走 multipart 上传接口，上限 50MiB）")
	return cmd
}

func newFunctionsDeploymentsListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list <function-id>",
		Short: "列出函数部署",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodFunctionsListDeployments, map[string]any{"functionId": args[0]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newFunctionsDeploymentsGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <function-id> <deployment-id>",
		Short: "按 ID 获取部署",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodFunctionsGetDeployment, map[string]any{"functionId": args[0], "deploymentId": args[1]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newFunctionsDeploymentsDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <function-id> <deployment-id>",
		Short: "删除部署",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := invoke(g, methodFunctionsDeleteDeployment, map[string]any{"functionId": args[0], "deploymentId": args[1]})
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// newFunctionsVariablesCmd: functions variables set/get。
func newFunctionsVariablesCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "variables",
		Short: "函数环境变量管理",
	}
	cmd.AddCommand(
		newFunctionsVariablesSetCmd(g),
		&cobra.Command{
			Use:   "get <function-id>",
			Short: "获取函数环境变量",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				resp, err := invoke(g, methodFunctionsGetVariables, map[string]any{"functionId": args[0]})
				if err != nil {
					return err
				}
				return printJSON(os.Stdout, resp)
			},
		},
	)
	return cmd
}

func newFunctionsVariablesSetCmd(g *globalFlags) *cobra.Command {
	var vars string
	cmd := &cobra.Command{
		Use:   "set <function-id> --vars '{...}'",
		Short: "全量替换环境变量（--vars 为 JSON 对象）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildSetVariablesReq(args[0], vars)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodFunctionsSetVariables, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&vars, "vars", "", "环境变量 JSON 对象（必填，如 '{\"FOO\":\"bar\"}'）")
	return cmd
}

// newFunctionsExecutionsCmd: functions executions create/list/get。
func newFunctionsExecutionsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "executions",
		Short: "函数执行管理",
	}
	cmd.AddCommand(
		newFunctionsExecutionsCreateCmd(g),
		&cobra.Command{
			Use:   "list <function-id>",
			Short: "列出执行记录（最近 100 条）",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				resp, err := invoke(g, methodFunctionsListExecutions, map[string]any{"functionId": args[0]})
				if err != nil {
					return err
				}
				return printJSON(os.Stdout, resp)
			},
		},
		&cobra.Command{
			Use:   "get <function-id> <execution-id>",
			Short: "按 ID 获取执行记录",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				resp, err := invoke(g, methodFunctionsGetExecution, map[string]any{"functionId": args[0], "executionId": args[1]})
				if err != nil {
					return err
				}
				return printJSON(os.Stdout, resp)
			},
		},
	)
	return cmd
}

func newFunctionsExecutionsCreateCmd(g *globalFlags) *cobra.Command {
	var input, deploymentID string
	var async bool
	cmd := &cobra.Command{
		Use:   "create <function-id> --input <json>",
		Short: "创建执行（缺省用最新 ready 部署）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateExecutionReq(cmd, args[0], input, deploymentID, async)
			if err != nil {
				return err
			}
			resp, err := invoke(g, methodFunctionsCreateExecution, req)
			if err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "执行输入（必填，须为合法 JSON 字符串，≤64KB）")
	cmd.Flags().StringVar(&deploymentID, "deployment-id", "", "指定部署（缺省用最新 ready 部署）")
	cmd.Flags().BoolVar(&async, "async", false, "异步执行（显式传 --async=true 才生效）")
	return cmd
}

// buildCreateFunctionReq 构造 CreateFunctionRequest（id/name/runtime 必填）。
func buildCreateFunctionReq(cmd *cobra.Command, id, name, runtime, entrypoint string, timeoutSeconds int32, spec string, enabled bool) (map[string]any, error) {
	if id == "" {
		return nil, fmt.Errorf("--id 必填")
	}
	if name == "" {
		return nil, fmt.Errorf("--name 必填")
	}
	if runtime == "" {
		return nil, fmt.Errorf("--runtime 必填（可用 runtimes 命令查看）")
	}
	req := map[string]any{"id": id, "name": name, "runtime": runtime}
	if entrypoint != "" {
		req["entrypoint"] = entrypoint
	}
	setChanged(cmd, "timeout-seconds", req, "timeoutSeconds", timeoutSeconds)
	setChanged(cmd, "spec", req, "spec", spec)
	setChanged(cmd, "enabled", req, "enabled", enabled)
	return req, nil
}

// buildUpdateFunctionReq 构造 UpdateFunctionRequest：仅设置显式传入的字段。
func buildUpdateFunctionReq(cmd *cobra.Command, functionID string, name, entrypoint string, timeoutSeconds int32, spec string, enabled bool) (map[string]any, error) {
	if functionID == "" {
		return nil, fmt.Errorf("缺少 function-id")
	}
	req := map[string]any{"functionId": functionID}
	setChanged(cmd, "name", req, "name", name)
	setChanged(cmd, "entrypoint", req, "entrypoint", entrypoint)
	setChanged(cmd, "timeout-seconds", req, "timeoutSeconds", timeoutSeconds)
	setChanged(cmd, "spec", req, "spec", spec)
	setChanged(cmd, "enabled", req, "enabled", enabled)
	return req, nil
}

// buildCreateDeploymentReq 读取 zip 文件并构造 CreateDeploymentRequest
// （code 为 bytes 字段，CLI 负责读文件后 base64 编码，不让用户手写）。
func buildCreateDeploymentReq(functionID, codePath string) (map[string]any, error) {
	if functionID == "" {
		return nil, fmt.Errorf("缺少 function-id")
	}
	if codePath == "" {
		return nil, fmt.Errorf("--code 必填（zip 代码包路径）")
	}
	code, err := os.ReadFile(codePath)
	if err != nil {
		return nil, fmt.Errorf("读取 --code 失败：%v", err)
	}
	if len(code) == 0 {
		return nil, fmt.Errorf("--code 为空文件")
	}
	if len(code) > 8<<20 {
		return nil, fmt.Errorf("--code 超过 8MiB（gRPC 消息通道上限，与服务端 MaxRecvMsgSize 对齐）；更大的代码包请走 multipart 上传接口（上限 50MiB）")
	}
	return map[string]any{"functionId": functionID, "code": base64.StdEncoding.EncodeToString(code)}, nil
}

// buildSetVariablesReq 构造 SetVariablesRequest（--vars 为 JSON 对象）。
func buildSetVariablesReq(functionID, vars string) (map[string]any, error) {
	if functionID == "" {
		return nil, fmt.Errorf("缺少 function-id")
	}
	kv, err := jsonStringMap(vars, "--vars")
	if err != nil {
		return nil, err
	}
	if len(kv) == 0 {
		return nil, fmt.Errorf("--vars 必填（环境变量 JSON 对象）")
	}
	list := make([]map[string]string, 0, len(kv))
	for k, v := range kv {
		list = append(list, map[string]string{"key": k, "value": v})
	}
	return map[string]any{"functionId": functionID, "variables": list}, nil
}

// buildCreateExecutionReq 构造 CreateExecutionRequest（--input 必填，与服务端
// 校验一致）。
func buildCreateExecutionReq(cmd *cobra.Command, functionID, input, deploymentID string, async bool) (map[string]any, error) {
	if functionID == "" {
		return nil, fmt.Errorf("缺少 function-id")
	}
	if input == "" {
		return nil, fmt.Errorf("--input 必填（执行输入 JSON 字符串）")
	}
	req := map[string]any{"functionId": functionID, "data": input}
	setChanged(cmd, "deployment-id", req, "deploymentId", deploymentID)
	setChanged(cmd, "async", req, "async", async)
	return req, nil
}
