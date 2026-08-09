package servergrpc

import (
	"context"
	"time"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	appfunctions "github.com/torchwooddev/torchwood/internal/app/functions"
	domainfunctions "github.com/torchwooddev/torchwood/internal/domain/functions"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type FunctionsService struct {
	serverv1.UnimplementedFunctionsServiceServer
	functions *appfunctions.Functions
}

func NewFunctionsService(functions *appfunctions.Functions) *FunctionsService {
	return &FunctionsService{functions: functions}
}

func (s *FunctionsService) projectID(ctx context.Context) (string, error) {
	p, ok := contexts.Principal(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing project context")
	}
	if p.ProjectID == "" {
		return "", status.Error(codes.Unauthenticated, "missing project context")
	}
	return p.ProjectID, nil
}

func (s *FunctionsService) ListRuntimes(ctx context.Context, _ *sharedv1.Empty) (*serverv1.ListRuntimesResponse, error) {
	if _, err := s.projectID(ctx); err != nil {
		return nil, err
	}
	out := s.functions.ListRuntimes()
	resp := &serverv1.ListRuntimesResponse{Runtimes: make([]*serverv1.RuntimeInfo, len(out))}
	for i, r := range out {
		resp.Runtimes[i] = &serverv1.RuntimeInfo{Id: r.ID, Name: r.Name, Entrypoint: r.Entrypoint}
	}
	return resp, nil
}

func (s *FunctionsService) ListSpecifications(ctx context.Context, _ *sharedv1.Empty) (*serverv1.ListSpecificationsResponse, error) {
	if _, err := s.projectID(ctx); err != nil {
		return nil, err
	}
	out := s.functions.ListSpecifications()
	resp := &serverv1.ListSpecificationsResponse{Specifications: make([]*serverv1.SpecificationInfo, len(out))}
	for i, sp := range out {
		resp.Specifications[i] = &serverv1.SpecificationInfo{Id: sp.ID, Cpu: sp.CPU, Memory: sp.Memory}
	}
	return resp, nil
}

func (s *FunctionsService) CreateFunction(ctx context.Context, req *serverv1.CreateFunctionRequest) (*serverv1.Function, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	ctx = contexts.WithAuditResource(ctx, req.GetId())
	fn, err := s.functions.CreateFunction(ctx, appfunctions.CreateFunctionCommand{
		ID:             req.GetId(),
		ProjectID:      projectID,
		Name:           req.GetName(),
		Runtime:        req.GetRuntime(),
		Entrypoint:     req.GetEntrypoint(),
		TimeoutSeconds: int(req.GetTimeoutSeconds()),
		Spec:           req.GetSpec(),
		Enabled:        req.GetEnabled(),
	})
	if err != nil {
		return nil, err
	}
	return mapFunction(fn), nil
}

func (s *FunctionsService) ListFunctions(ctx context.Context, _ *sharedv1.ListRequest) (*serverv1.ListFunctionsResponse, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	fns, err := s.functions.ListFunctions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	resp := &serverv1.ListFunctionsResponse{
		Functions: make([]*serverv1.Function, len(fns)),
		Meta:      &sharedv1.ListResponseMeta{},
	}
	for i := range fns {
		resp.Functions[i] = mapFunction(&fns[i])
	}
	return resp, nil
}

func (s *FunctionsService) GetFunction(ctx context.Context, req *serverv1.GetFunctionRequest) (*serverv1.Function, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	fn, err := s.functions.GetFunction(ctx, projectID, req.GetFunctionId())
	if err != nil {
		return nil, err
	}
	return mapFunction(fn), nil
}

func (s *FunctionsService) UpdateFunction(ctx context.Context, req *serverv1.UpdateFunctionRequest) (*serverv1.Function, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	ctx = contexts.WithAuditResource(ctx, req.GetFunctionId())
	cmd := appfunctions.UpdateFunctionCommand{
		ProjectID:  projectID,
		FunctionID: req.GetFunctionId(),
	}
	if req.Name != nil {
		cmd.Name = req.Name
	}
	if req.Entrypoint != nil {
		cmd.Entrypoint = req.Entrypoint
	}
	if req.TimeoutSeconds != nil {
		t := int(req.GetTimeoutSeconds())
		cmd.TimeoutSeconds = &t
	}
	if req.Spec != nil {
		cmd.Spec = req.Spec
	}
	if req.Enabled != nil {
		cmd.Enabled = req.Enabled
	}
	fn, err := s.functions.UpdateFunction(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return mapFunction(fn), nil
}

func (s *FunctionsService) DeleteFunction(ctx context.Context, req *serverv1.GetFunctionRequest) (*sharedv1.Empty, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	ctx = contexts.WithAuditResource(ctx, req.GetFunctionId())
	if err := s.functions.DeleteFunction(ctx, projectID, req.GetFunctionId()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *FunctionsService) CreateDeployment(ctx context.Context, req *serverv1.CreateDeploymentRequest) (*serverv1.Deployment, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	ctx = contexts.WithAuditResource(ctx, req.GetFunctionId())
	dep, err := s.functions.CreateDeployment(ctx, appfunctions.CreateDeploymentCommand{
		ProjectID:  projectID,
		FunctionID: req.GetFunctionId(),
		Code:       req.GetCode(),
	})
	if err != nil {
		return nil, err
	}
	return mapDeployment(dep), nil
}

func (s *FunctionsService) ListDeployments(ctx context.Context, req *serverv1.GetFunctionRequest) (*serverv1.ListDeploymentsResponse, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	deps, err := s.functions.ListDeployments(ctx, projectID, req.GetFunctionId())
	if err != nil {
		return nil, err
	}
	resp := &serverv1.ListDeploymentsResponse{Deployments: make([]*serverv1.Deployment, len(deps))}
	for i := range deps {
		resp.Deployments[i] = mapDeployment(&deps[i])
	}
	return resp, nil
}

func (s *FunctionsService) GetDeployment(ctx context.Context, req *serverv1.GetDeploymentRequest) (*serverv1.Deployment, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	dep, err := s.functions.GetDeployment(ctx, projectID, req.GetFunctionId(), req.GetDeploymentId())
	if err != nil {
		return nil, err
	}
	return mapDeployment(dep), nil
}

func (s *FunctionsService) DeleteDeployment(ctx context.Context, req *serverv1.GetDeploymentRequest) (*sharedv1.Empty, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	ctx = contexts.WithAuditResource(ctx, req.GetFunctionId()+"/deployments/"+req.GetDeploymentId())
	if err := s.functions.DeleteDeployment(ctx, projectID, req.GetFunctionId(), req.GetDeploymentId()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *FunctionsService) SetVariables(ctx context.Context, req *serverv1.SetVariablesRequest) (*serverv1.Variables, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	ctx = contexts.WithAuditResource(ctx, req.GetFunctionId())
	vars := make(map[string]string, len(req.GetVariables()))
	for _, v := range req.GetVariables() {
		vars[v.GetKey()] = v.GetValue()
	}
	out, err := s.functions.SetVariables(ctx, projectID, req.GetFunctionId(), vars)
	if err != nil {
		return nil, err
	}
	return mapVariables(out), nil
}

func (s *FunctionsService) GetVariables(ctx context.Context, req *serverv1.GetFunctionRequest) (*serverv1.Variables, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	vars, err := s.functions.GetVariables(ctx, projectID, req.GetFunctionId())
	if err != nil {
		return nil, err
	}
	return mapVariables(vars), nil
}

func (s *FunctionsService) CreateExecution(ctx context.Context, req *serverv1.CreateExecutionRequest) (*serverv1.Execution, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	ctx = contexts.WithAuditResource(ctx, req.GetFunctionId()+"/executions")
	cmd := appfunctions.CreateExecutionCommand{
		ProjectID:  projectID,
		FunctionID: req.GetFunctionId(),
		Data:       req.GetData(),
		Async:      req.GetAsync(),
	}
	if req.DeploymentId != nil {
		cmd.DeploymentID = req.GetDeploymentId()
	}

	if !cmd.Async {
		// 同步执行兜底超时：fn.TimeoutSeconds + 60s 余量（gRPC 直连路径），
		// 超时映射 DeadlineExceeded（HTTP 504）。
		fn, err := s.functions.GetFunction(ctx, projectID, req.GetFunctionId())
		if err != nil {
			return nil, err
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(fn.TimeoutSeconds)*time.Second+60*time.Second)
		defer cancel()
	}

	rec, err := s.functions.CreateExecution(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return mapExecution(rec), nil
}

func (s *FunctionsService) ListExecutions(ctx context.Context, req *serverv1.GetFunctionRequest) (*serverv1.ListExecutionsResponse, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	recs, err := s.functions.ListExecutions(ctx, projectID, req.GetFunctionId())
	if err != nil {
		return nil, err
	}
	resp := &serverv1.ListExecutionsResponse{Executions: make([]*serverv1.Execution, len(recs))}
	for i := range recs {
		resp.Executions[i] = mapExecution(&recs[i])
	}
	return resp, nil
}

func (s *FunctionsService) GetExecution(ctx context.Context, req *serverv1.GetExecutionRequest) (*serverv1.Execution, error) {
	projectID, err := s.projectID(ctx)
	if err != nil {
		return nil, err
	}
	rec, err := s.functions.GetExecution(ctx, projectID, req.GetFunctionId(), req.GetExecutionId())
	if err != nil {
		return nil, err
	}
	return mapExecution(rec), nil
}

func mapFunction(fn *domainfunctions.Function) *serverv1.Function {
	if fn == nil {
		return nil
	}
	return &serverv1.Function{
		Id:             fn.ID,
		ProjectId:      fn.ProjectID,
		Name:           fn.Name,
		Runtime:        fn.Runtime,
		Entrypoint:     fn.Entrypoint,
		TimeoutSeconds: int32(fn.TimeoutSeconds),
		Spec:           fn.Spec,
		Enabled:        fn.Enabled,
		CreatedAt:      timestamppb.New(fn.CreatedAt),
		UpdatedAt:      timestamppb.New(fn.UpdatedAt),
	}
}

func mapDeployment(d *domainfunctions.Deployment) *serverv1.Deployment {
	if d == nil {
		return nil
	}
	return &serverv1.Deployment{
		Id:         d.ID,
		FunctionId: d.FunctionID,
		Size:       d.Size,
		Status:     d.Status,
		Error:      d.Error,
		CreatedAt:  timestamppb.New(d.CreatedAt),
		UpdatedAt:  timestamppb.New(d.UpdatedAt),
	}
}

func mapVariables(vars map[string]string) *serverv1.Variables {
	out := &serverv1.Variables{}
	for k, v := range vars {
		out.Variables = append(out.Variables, &serverv1.Variable{Key: k, Value: v})
	}
	return out
}

func mapExecution(rec *domainfunctions.ExecutionRecord) *serverv1.Execution {
	if rec == nil {
		return nil
	}
	return &serverv1.Execution{
		Id:                rec.ID,
		FunctionId:        rec.FunctionID,
		DeploymentId:      rec.DeploymentID,
		Status:            rec.Status,
		Response:          rec.Response,
		Stdout:            rec.Stdout,
		Stderr:            rec.Stderr,
		StatusCode:        int32(rec.StatusCode),
		DurationMs:        rec.DurationMS,
		Error:             rec.Error,
		ResponseTruncated: rec.ResponseTruncated,
		StdoutTruncated:   rec.StdoutTruncated,
		StderrTruncated:   rec.StderrTruncated,
		CreatedAt:         timestamppb.New(rec.CreatedAt),
		UpdatedAt:         timestamppb.New(rec.UpdatedAt),
	}
}
