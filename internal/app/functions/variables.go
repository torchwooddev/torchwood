package functions

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// secretMask 是 GetVariables 返回的 secret 值掩码（值仅在 SetVariables 可见）。
const secretMask = "******"

// SetVariables 全量替换函数环境变量（校验键名与总量上限）。
func (f *Functions) SetVariables(ctx context.Context, projectID, functionID string, vars map[string]string) (map[string]string, error) {
	fn, err := f.repo.GetFunction(ctx, projectID, functionID)
	if err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, status.Error(codes.NotFound, "function not found")
	}
	if envSize(vars) > maxEnvBytes {
		return nil, status.Errorf(codes.InvalidArgument, "environment variables exceed maximum total size of %d bytes", maxEnvBytes)
	}
	for k := range vars {
		if strings.TrimSpace(k) == "" || strings.ContainsAny(k, "\n\r\x00") {
			return nil, status.Errorf(codes.InvalidArgument, "invalid variable key %q", k)
		}
	}
	if err := f.repo.SetVariables(ctx, projectID, functionID, vars); err != nil {
		return nil, err
	}
	return vars, nil
}

func (f *Functions) GetVariables(ctx context.Context, projectID, functionID string) (map[string]string, error) {
	fn, err := f.repo.GetFunction(ctx, projectID, functionID)
	if err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, status.Error(codes.NotFound, "function not found")
	}
	vars, err := f.repo.GetVariables(ctx, projectID, functionID)
	if err != nil {
		return nil, err
	}
	for k, v := range vars {
		if v != "" {
			vars[k] = secretMask
		}
	}
	return vars, nil
}
