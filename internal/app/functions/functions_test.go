package functions

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	domainfunctions "github.com/torchwooddev/torchwood/internal/domain/functions"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
)

type mockExecutor struct {
	calls  []domainfunctions.Execution
	result *domainfunctions.ExecutionResult
	err    error
}

func (m *mockExecutor) Execute(_ context.Context, exec domainfunctions.Execution) (*domainfunctions.ExecutionResult, error) {
	m.calls = append(m.calls, exec)
	return m.result, m.err
}

func newMockExecutor(result *domainfunctions.ExecutionResult, err error) *mockExecutor {
	return &mockExecutor{result: result, err: err}
}

func TestExecute_DelegatesWithDefaults(t *testing.T) {
	executor := newMockExecutor(&domainfunctions.ExecutionResult{
		StatusCode: 200,
		Stdout:     "hello",
		Response:   `{"ok":true}`,
	}, nil)
	uc := NewFunctions(&config.AppConfig{}, executor)

	res, err := uc.Execute(context.Background(), ExecuteCommand{
		FunctionID: "fn_1",
		SourcePath: "/tmp/src.zip",
		Env:        map[string]string{"FOO": "bar"},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, 200, res.StatusCode)
	require.Equal(t, "hello", res.Stdout)

	require.Len(t, executor.calls, 1)
	exec := executor.calls[0]
	require.Equal(t, "fn_1", exec.FunctionID)
	require.Equal(t, "node-18.0", exec.Runtime, "runtime defaults to node-18.0")
	require.Equal(t, "index.main", exec.Entrypoint, "entrypoint defaults to index.main")
	require.Equal(t, int64(15), exec.Timeout, "timeout defaults to 15s")
	require.Equal(t, map[string]string{"FOO": "bar"}, exec.Env)
}

func TestExecute_PassesThroughExplicitFields(t *testing.T) {
	executor := newMockExecutor(nil, errors.New("boom"))
	uc := NewFunctions(&config.AppConfig{}, executor)

	_, err := uc.Execute(context.Background(), ExecuteCommand{
		FunctionID: "fn_2",
		Runtime:    "python-3.11",
		Entrypoint: "main.handler",
		Timeout:    60,
		Env:        map[string]string{"A": "b"},
	})
	require.ErrorContains(t, err, "boom")

	require.Len(t, executor.calls, 1)
	exec := executor.calls[0]
	require.Equal(t, "python-3.11", exec.Runtime)
	require.Equal(t, "main.handler", exec.Entrypoint)
	require.Equal(t, int64(60), exec.Timeout)
}

func TestSanitizeEnv(t *testing.T) {
	env := map[string]string{
		"OK":     "fine",
		"a\nb":   "newline in key",
		"a\rb":   "carriage return in key",
		"a\x00b": "nul in key",
		"SPACE":  "a b",
		"EMPTY":  "",
	}
	got := sanitizeEnv(env)
	require.Equal(t, map[string]string{"OK": "fine", "SPACE": "a b", "EMPTY": ""}, got)
}

func TestRuntimeImage(t *testing.T) {
	cfg := &config.AppConfig{}
	uc := NewFunctions(cfg, newMockExecutor(nil, nil))
	require.Equal(t, "Torchwood/runtime-node-18.0:latest", uc.RuntimeImage("node-18.0"), "default registry is Torchwood")

	cfg.Functions = &config.Functions{
		Docker: &config.Functions_Docker{Registry: "ghcr.io/torchwood"},
	}
	require.Equal(t, "ghcr.io/torchwood/runtime-node-18.0:latest", uc.RuntimeImage("node-18.0"))
}
