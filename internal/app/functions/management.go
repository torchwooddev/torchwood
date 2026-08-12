package functions

import (
	"context"
	"regexp"
	"strings"
	"time"

	domainfunctions "github.com/torchwooddev/torchwood/internal/domain/functions"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// functionIDPattern 限制 Function ID 字符集与长度（防路径穿越拼入 zip 路径
// 与镜像名；须以字母数字开头，仅含字母数字/下划线/连字符，最长 64）。
var functionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// functionIDReserved 是 REST 字面量路由段，function_id 不得取这些值：
// GET /v1/server/functions/runtimes、/specifications 为字面量路由，grpc-gateway
// 字面量优先匹配，同名 function 经 REST 永远无法访问（F11-3，方案 B）。
var functionIDReserved = map[string]struct{}{
	"runtimes":       {},
	"specifications": {},
}

const (
	// minTimeoutSeconds / maxTimeoutSeconds 是函数超时允许范围（§5.2）。
	minTimeoutSeconds = 1
	maxTimeoutSeconds = 300
	// maxSyncTimeoutSeconds 限制同步执行超时（grpc-gateway WriteTimeout 余量）。
	maxSyncTimeoutSeconds = 30
	// defaultTimeoutSeconds 是创建函数未显式指定 timeout_seconds 时的服务端默认值
	// （与 Console 前端默认值 15 及 DB 列 DEFAULT 15 保持一致）。
	defaultTimeoutSeconds = 15
)

type CreateFunctionCommand struct {
	ID             string
	ProjectID      string
	Name           string
	Runtime        string
	Entrypoint     string
	TimeoutSeconds *int
	Spec           string
	Enabled        *bool
}

type UpdateFunctionCommand struct {
	ProjectID      string
	FunctionID     string
	Name           *string
	Entrypoint     *string
	TimeoutSeconds *int
	Spec           *string
	Enabled        *bool
}

func (f *Functions) CreateFunction(ctx context.Context, cmd CreateFunctionCommand) (*domainfunctions.Function, error) {
	if !idgen.ID(cmd.ID).IsValid() {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if !functionIDPattern.MatchString(cmd.ID) {
		return nil, status.Error(codes.InvalidArgument, "invalid function id: must match ^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$")
	}
	if _, reserved := functionIDReserved[cmd.ID]; reserved {
		return nil, status.Errorf(codes.InvalidArgument, "function id %q is reserved", cmd.ID)
	}
	if strings.TrimSpace(cmd.Name) == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if !runtimeExists(cmd.Runtime) {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported runtime %q", cmd.Runtime)
	}
	timeoutSeconds := defaultTimeoutSeconds
	if cmd.TimeoutSeconds != nil {
		timeoutSeconds = *cmd.TimeoutSeconds
	}
	if timeoutSeconds < minTimeoutSeconds || timeoutSeconds > maxTimeoutSeconds {
		return nil, status.Errorf(codes.InvalidArgument, "timeout_seconds must be between %d and %d", minTimeoutSeconds, maxTimeoutSeconds)
	}
	if cmd.Spec == "" {
		cmd.Spec = "shared-1x"
	}
	if !specificationExists(cmd.Spec) {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported spec %q", cmd.Spec)
	}
	if cmd.Entrypoint == "" {
		cmd.Entrypoint = defaultEntrypoint(cmd.Runtime)
	}
	enabled := true
	if cmd.Enabled != nil {
		enabled = *cmd.Enabled
	}
	now := time.Now()
	fn := &domainfunctions.Function{
		ID:             cmd.ID,
		ProjectID:      cmd.ProjectID,
		Name:           cmd.Name,
		Runtime:        cmd.Runtime,
		Entrypoint:     cmd.Entrypoint,
		TimeoutSeconds: timeoutSeconds,
		Spec:           cmd.Spec,
		Enabled:        enabled,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := f.repo.CreateFunction(ctx, fn); err != nil {
		return nil, err
	}
	return fn, nil
}

func (f *Functions) ListFunctions(ctx context.Context, projectID string) ([]domainfunctions.Function, error) {
	return f.repo.ListFunctions(ctx, projectID)
}

func (f *Functions) GetFunction(ctx context.Context, projectID, functionID string) (*domainfunctions.Function, error) {
	fn, err := f.repo.GetFunction(ctx, projectID, functionID)
	if err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, status.Error(codes.NotFound, "function not found")
	}
	return fn, nil
}

func (f *Functions) UpdateFunction(ctx context.Context, cmd UpdateFunctionCommand) (*domainfunctions.Function, error) {
	fn, err := f.repo.GetFunction(ctx, cmd.ProjectID, cmd.FunctionID)
	if err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, status.Error(codes.NotFound, "function not found")
	}
	if cmd.Name != nil {
		if strings.TrimSpace(*cmd.Name) == "" {
			return nil, status.Error(codes.InvalidArgument, "name is required")
		}
		fn.Name = *cmd.Name
	}
	if cmd.Entrypoint != nil {
		fn.Entrypoint = *cmd.Entrypoint
	}
	if cmd.TimeoutSeconds != nil {
		if *cmd.TimeoutSeconds < minTimeoutSeconds || *cmd.TimeoutSeconds > maxTimeoutSeconds {
			return nil, status.Errorf(codes.InvalidArgument, "timeout_seconds must be between %d and %d", minTimeoutSeconds, maxTimeoutSeconds)
		}
		fn.TimeoutSeconds = *cmd.TimeoutSeconds
	}
	if cmd.Spec != nil {
		if !specificationExists(*cmd.Spec) {
			return nil, status.Errorf(codes.InvalidArgument, "unsupported spec %q", *cmd.Spec)
		}
		fn.Spec = *cmd.Spec
	}
	if cmd.Enabled != nil {
		fn.Enabled = *cmd.Enabled
	}
	fn.UpdatedAt = time.Now()
	if err := f.repo.UpdateFunction(ctx, fn); err != nil {
		return nil, err
	}
	return fn, nil
}

func (f *Functions) DeleteFunction(ctx context.Context, projectID, functionID string) error {
	fn, err := f.repo.GetFunction(ctx, projectID, functionID)
	if err != nil {
		return err
	}
	if fn == nil {
		return status.Error(codes.NotFound, "function not found")
	}
	deps, err := f.repo.ListDeployments(ctx, projectID, functionID)
	if err != nil {
		return err
	}
	// 先 DB 级联删除，再清理镜像与本地 zip（全部幂等，失败仅记日志）。
	if err := f.repo.DeleteFunction(ctx, projectID, functionID); err != nil {
		return err
	}
	for i := range deps {
		_ = f.executor.RemoveImage(ctx, deps[i].FunctionID, deps[i].ID)
		_ = removeZip(deps[i].ProjectID, deps[i].FunctionID, deps[i].ID)
	}
	return nil
}
