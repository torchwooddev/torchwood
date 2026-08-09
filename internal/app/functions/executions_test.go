package functions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	domainfunctions "github.com/torchwooddev/torchwood/internal/domain/functions"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newTestUC(executor *mockExecutor, repo *mockRepo, queue *mockQueue) *Functions {
	return NewFunctions(&config.AppConfig{}, executor, repo, queue)
}

func TestCreateExecution_SyncWritesResult(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	executor := newMockExecutor(&domainfunctions.ExecutionResult{
		StatusCode: 0,
		Stdout:     "hello",
		Response:   `{"ok":true}`,
		DurationMS: 42,
	}, nil)
	uc := newTestUC(executor, repo, newMockQueue())

	rec, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{
		ProjectID:  "p1",
		FunctionID: "fn_1",
		Data:       `{"a":1}`,
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, domainfunctions.ExecutionStatusCompleted, rec.Status)
	require.Equal(t, "hello", rec.Stdout)
	require.Equal(t, `{"ok":true}`, rec.Response)
	require.Equal(t, int64(42), rec.DurationMS)
	require.Equal(t, "dep_ready", rec.DeploymentID)
	require.Len(t, executor.calls, 1)
	require.Equal(t, `{"a":1}`, executor.calls[0].Data, "data 透传给 executor")
}

func TestCreateExecution_SyncTruncatesOutput(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	big := string(make([]byte, maxOutputBytes+1024))
	executor := newMockExecutor(&domainfunctions.ExecutionResult{StatusCode: 0, Stdout: big}, nil)
	uc := newTestUC(executor, repo, newMockQueue())

	rec, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{ProjectID: "p1", FunctionID: "fn_1"})
	require.NoError(t, err)
	require.Equal(t, domainfunctions.ExecutionStatusCompleted, rec.Status)
	require.Len(t, rec.Stdout, maxOutputBytes)
	require.True(t, rec.StdoutTruncated)
	require.False(t, rec.ResponseTruncated)
}

func TestCreateExecution_SyncTimeoutMarksFailed(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	executor := newMockExecutor(nil, context.DeadlineExceeded)
	uc := newTestUC(executor, repo, newMockQueue())

	_, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{ProjectID: "p1", FunctionID: "fn_1"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Len(t, repo.executions, 1)
	for _, e := range repo.executions {
		require.Equal(t, domainfunctions.ExecutionStatusFailed, e.Status)
		require.Equal(t, "execution timed out", e.Error)
	}
}

func TestCreateExecution_SyncExitCodeNonZeroFails(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	executor := newMockExecutor(&domainfunctions.ExecutionResult{StatusCode: 1, Stderr: "boom"}, nil)
	uc := newTestUC(executor, repo, newMockQueue())

	rec, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{ProjectID: "p1", FunctionID: "fn_1"})
	require.NoError(t, err)
	require.Equal(t, domainfunctions.ExecutionStatusFailed, rec.Status)
	require.Equal(t, "boom", rec.Error)
}

func TestCreateExecution_AsyncEnqueues(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	q := newMockQueue()
	uc := newTestUC(newMockExecutor(nil, nil), repo, q)

	rec, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{
		ProjectID:  "p1",
		FunctionID: "fn_1",
		Data:       `{"x":1}`,
		Async:      true,
	})
	require.NoError(t, err)
	require.Equal(t, domainfunctions.ExecutionStatusQueued, rec.Status)
	require.Len(t, q.enqueued, 1)
	var msg queueMessage
	require.NoError(t, json.Unmarshal(q.enqueued[0], &msg))
	require.Equal(t, rec.ID, msg.ExecutionID)
	require.Equal(t, "fn_1", msg.FunctionID)
	require.Equal(t, "p1", msg.ProjectID)
	require.Equal(t, `{"x":1}`, msg.Data, "data 随队列 payload 传递")
}

func TestCreateExecution_EnqueueFailureMarksFailed(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	q := newMockQueue()
	q.err = errors.New("redis down")
	uc := newTestUC(newMockExecutor(nil, nil), repo, q)

	_, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{
		ProjectID:  "p1",
		FunctionID: "fn_1",
		Async:      true,
	})
	require.Error(t, err)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Len(t, repo.executions, 1)
	var rec *domainfunctions.ExecutionRecord
	for _, e := range repo.executions {
		rec = e
	}
	require.Equal(t, domainfunctions.ExecutionStatusFailed, rec.Status)
	require.Equal(t, "enqueue failed", rec.Error)
}

func TestCreateExecution_NoReadyDeployment(t *testing.T) {
	repo := newMockRepo()
	fn := seedReadyFunction(repo, "p1", "fn_1", true, 15)
	// 移除 ready 部署。
	require.NoError(t, repo.DeleteDeployment(context.Background(), fn.ID, "dep_ready"))
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	_, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{ProjectID: "p1", FunctionID: "fn_1"})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.ErrorContains(t, err, "no ready deployment")
}

func TestCreateExecution_ExplicitDeploymentNotReady(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	require.NoError(t, repo.CreateDeployment(context.Background(), &domainfunctions.Deployment{
		ID: "dep_pending", FunctionID: "fn_1", ProjectID: "p1", Status: domainfunctions.DeploymentStatusPending,
	}))
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	_, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{
		ProjectID: "p1", FunctionID: "fn_1", DeploymentID: "dep_pending",
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.ErrorContains(t, err, "not ready")
}

func TestCreateExecution_DataTooLarge(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	big := make([]byte, maxExecutionDataBytes+1)
	_, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{
		ProjectID: "p1", FunctionID: "fn_1", Data: string(big),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateExecution_InvalidJSON(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	_, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{
		ProjectID: "p1", FunctionID: "fn_1", Data: "{not json",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateExecution_SyncTimeoutOver30Rejected(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 60)
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	_, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{ProjectID: "p1", FunctionID: "fn_1"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "use async")

	// 异步不受 30s 限制。
	rec, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{ProjectID: "p1", FunctionID: "fn_1", Async: true})
	require.NoError(t, err)
	require.NotNil(t, rec)
}

func TestCreateExecution_DisabledFunction(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", false, 15)
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	_, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{ProjectID: "p1", FunctionID: "fn_1"})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestCreateExecution_NotFound(t *testing.T) {
	repo := newMockRepo()
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	_, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{ProjectID: "p1", FunctionID: "nope"})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestCreateExecution_ResourceExhausted(t *testing.T) {
	// 占满运行信号量。
	for i := 0; i < maxConcurrentRuns; i++ {
		runSemaphore <- struct{}{}
	}
	defer func() {
		for i := 0; i < maxConcurrentRuns; i++ {
			<-runSemaphore
		}
	}()

	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	_, err := uc.CreateExecution(context.Background(), CreateExecutionCommand{ProjectID: "p1", FunctionID: "fn_1"})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Len(t, repo.executions, 1)
	for _, e := range repo.executions {
		require.Equal(t, domainfunctions.ExecutionStatusFailed, e.Status)
		require.Equal(t, "too many concurrent executions", e.Error)
	}
}

func TestCreateFunction_Validation(t *testing.T) {
	repo := newMockRepo()
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())
	ctx := context.Background()

	_, err := uc.CreateFunction(ctx, CreateFunctionCommand{ID: "fn_x", ProjectID: "p1", Name: "f", Runtime: "bogus", TimeoutSeconds: 15})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "unsupported runtime")

	_, err = uc.CreateFunction(ctx, CreateFunctionCommand{ID: "fn_x", ProjectID: "p1", Name: "f", Runtime: "node-18.0", TimeoutSeconds: 0})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = uc.CreateFunction(ctx, CreateFunctionCommand{ID: "fn_x", ProjectID: "p1", Name: "f", Runtime: "node-18.0", TimeoutSeconds: 301})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = uc.CreateFunction(ctx, CreateFunctionCommand{ID: "fn_x", ProjectID: "p1", Name: "f", Runtime: "node-18.0", TimeoutSeconds: 15, Spec: "bogus"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "unsupported spec")

	_, err = uc.CreateFunction(ctx, CreateFunctionCommand{ID: "fn_x", ProjectID: "p1", Name: "", Runtime: "node-18.0", TimeoutSeconds: 15})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = uc.CreateFunction(ctx, CreateFunctionCommand{ProjectID: "p1", Name: "f", Runtime: "node-18.0", TimeoutSeconds: 15})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "id is required")

	fn, err := uc.CreateFunction(ctx, CreateFunctionCommand{
		ID: "fn_new", ProjectID: "p1", Name: "f", Runtime: "python-3.11", TimeoutSeconds: 15,
	})
	require.NoError(t, err)
	require.Equal(t, "shared-1x", fn.Spec, "spec 缺省 shared-1x")
	require.Equal(t, "main.main", fn.Entrypoint, "python 缺省 entrypoint")
}

func TestProcessExecution_RebuildsWhenDeploymentNotReady(t *testing.T) {
	repo := newMockRepo()
	fn := seedReadyFunction(repo, "p1", "fn_1", true, 15)
	require.NoError(t, repo.DeleteDeployment(context.Background(), fn.ID, "dep_ready"))
	require.NoError(t, repo.CreateDeployment(context.Background(), &domainfunctions.Deployment{
		ID: "dep_pending", FunctionID: "fn_1", ProjectID: "p1", Status: domainfunctions.DeploymentStatusPending,
	}))

	executor := newMockExecutor(&domainfunctions.ExecutionResult{StatusCode: 0, Stdout: "ok"}, nil)
	uc := newTestUC(executor, repo, newMockQueue())

	rec := &domainfunctions.ExecutionRecord{
		ID: "e1", FunctionID: "fn_1", ProjectID: "p1", DeploymentID: "dep_pending",
		Status: domainfunctions.ExecutionStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, repo.CreateExecution(context.Background(), rec))

	err := uc.ProcessExecutionPayload(context.Background(), []byte(fmt.Sprintf(
		`{"execution_id":"e1","function_id":"fn_1","project_id":"p1","data":"{}"}`)))
	require.NoError(t, err)
	require.Equal(t, 1, executor.builds, "deployment 非 ready 时补构建")
	dep, _ := repo.GetDeployment(context.Background(), "fn_1", "dep_pending")
	require.Equal(t, domainfunctions.DeploymentStatusReady, dep.Status)
	got, _ := repo.GetExecution(context.Background(), "p1", "fn_1", "e1")
	require.Equal(t, domainfunctions.ExecutionStatusCompleted, got.Status)
}

func TestProcessExecution_MissingExecutionSilentlyIgnored(t *testing.T) {
	repo := newMockRepo()
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())
	err := uc.ProcessExecutionPayload(context.Background(), []byte(
		`{"execution_id":"ghost","function_id":"fn_1","project_id":"p1"}`))
	require.NoError(t, err)
}

func TestProcessExecution_InvalidPayload(t *testing.T) {
	repo := newMockRepo()
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())
	err := uc.ProcessExecutionPayload(context.Background(), []byte("not json"))
	require.Error(t, err)
}
