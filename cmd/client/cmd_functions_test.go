package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"google.golang.org/protobuf/proto"
)

func int32Ptr(v int32) *int32 { return &v }

func TestBuildCreateFunctionReq(t *testing.T) {
	tests := []struct {
		name           string
		id             string
		functionName   string
		runtime        string
		entrypoint     string
		timeoutSeconds *int32
		spec           *string
		enabled        *bool
		wantErr        string
	}{
		{name: "缺 id", functionName: "f", runtime: "nodejs18", wantErr: "--id 必填"},
		{name: "缺 name", id: "f1", runtime: "nodejs18", wantErr: "--name 必填"},
		{name: "缺 runtime", id: "f1", functionName: "f", wantErr: "--runtime 必填"},
		{name: "最小字段", id: "f1", functionName: "f", runtime: "nodejs18", wantErr: ""},
		{name: "全字段", id: "f1", functionName: "f", runtime: "nodejs18", entrypoint: "index.js",
			timeoutSeconds: int32Ptr(30), spec: stringPtr("shared-2x"), enabled: boolPtr(true), wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateFunctionReq(tt.id, tt.functionName, tt.runtime, tt.entrypoint,
				tt.timeoutSeconds, tt.spec, tt.enabled)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetId() != tt.id || req.GetName() != tt.functionName || req.GetRuntime() != tt.runtime {
				t.Errorf("请求不匹配: %v", req)
			}
			if tt.timeoutSeconds != nil && (req.TimeoutSeconds == nil || *req.TimeoutSeconds != *tt.timeoutSeconds) {
				t.Errorf("timeoutSeconds 不匹配: %v", req.TimeoutSeconds)
			}
			if tt.spec != nil && (req.Spec == nil || *req.Spec != *tt.spec) {
				t.Errorf("spec 不匹配: %v", req.Spec)
			}
			if tt.enabled != nil && (req.Enabled == nil || *req.Enabled != *tt.enabled) {
				t.Errorf("enabled 不匹配: %v", req.Enabled)
			}
		})
	}
}

func stringPtr(s string) *string { return &s }

func TestBuildUpdateFunctionReq(t *testing.T) {
	tests := []struct {
		name           string
		functionID     string
		newName        *string
		entrypoint     *string
		timeoutSeconds *int32
		spec           *string
		enabled        *bool
		wantErr        string
	}{
		{name: "缺 function-id", wantErr: "缺少 function-id"},
		{name: "仅 name", functionID: "f1", newName: stringPtr("new"), wantErr: ""},
		{name: "全字段", functionID: "f1", newName: stringPtr("new"), entrypoint: stringPtr("main.py"),
			timeoutSeconds: int32Ptr(60), spec: stringPtr("shared-1x"), enabled: boolPtr(false), wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildUpdateFunctionReq(tt.functionID, tt.newName, tt.entrypoint,
				tt.timeoutSeconds, tt.spec, tt.enabled)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.newName == nil && req.GetName() != "" {
				t.Errorf("未传 --name 不应设置: %q", req.GetName())
			}
			if tt.newName != nil && (req.Name == nil || *req.Name != *tt.newName) {
				t.Errorf("name 不匹配: %v", req.Name)
			}
			if tt.timeoutSeconds != nil && (req.TimeoutSeconds == nil || *req.TimeoutSeconds != *tt.timeoutSeconds) {
				t.Errorf("timeoutSeconds 不匹配: %v", req.TimeoutSeconds)
			}
		})
	}
}

func TestBuildCreateDeploymentReq(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "code.zip")
	if err := os.WriteFile(good, []byte("PK\x03\x04fakezip"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		functionID string
		codePath   string
		wantErr    string
	}{
		{name: "缺 function-id", codePath: good, wantErr: "缺少 function-id"},
		{name: "缺 code", functionID: "f1", wantErr: "--code 必填"},
		{name: "文件不存在", functionID: "f1", codePath: filepath.Join(dir, "nope.zip"),
			wantErr: "读取 --code 失败"},
		{name: "正常", functionID: "f1", codePath: good, wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateDeploymentReq(tt.functionID, tt.codePath)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(req.GetCode()) != "PK\x03\x04fakezip" {
				t.Errorf("code 字节未正确读取")
			}
		})
	}
}

func TestBuildSetVariablesReq(t *testing.T) {
	tests := []struct {
		name       string
		functionID string
		vars       string
		wantErr    string
	}{
		{name: "缺 vars", functionID: "f1", wantErr: "--vars 必填"},
		{name: "vars 非法", functionID: "f1", vars: `[1]`, wantErr: "--vars 解析失败"},
		{name: "正常", functionID: "f1", vars: `{"FOO":"bar"}`, wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildSetVariablesReq(tt.functionID, tt.vars)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(req.GetVariables()) != 1 || req.GetVariables()[0].GetKey() != "FOO" || req.GetVariables()[0].GetValue() != "bar" {
				t.Errorf("variables 未解析: %v", req.GetVariables())
			}
		})
	}
}

func TestBuildCreateExecutionReq(t *testing.T) {
	tests := []struct {
		name         string
		functionID   string
		input        string
		deploymentID *string
		async        *bool
		wantErr      string
	}{
		{name: "缺 input", functionID: "f1", wantErr: "--input 必填"},
		{name: "同步最小字段", functionID: "f1", input: `{"a":1}`, wantErr: ""},
		{name: "异步指定部署", functionID: "f1", input: `{}`, deploymentID: stringPtr("d1"),
			async: boolPtr(true), wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateExecutionReq(tt.functionID, tt.input, tt.deploymentID, tt.async)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetData() != tt.input {
				t.Errorf("data 不匹配: %q", req.GetData())
			}
			if tt.deploymentID == nil && req.GetDeploymentId() != "" {
				t.Errorf("deploymentId 不应设置: %q", req.GetDeploymentId())
			}
			if tt.deploymentID != nil && (req.DeploymentId == nil || *req.DeploymentId != *tt.deploymentID) {
				t.Errorf("deploymentId 不匹配: %v", req.DeploymentId)
			}
			if tt.async != nil && (req.Async == nil || *req.Async != *tt.async) {
				t.Errorf("async 不匹配: %v", req.Async)
			}
		})
	}
}

// TestFunctionsRegistryTypes 校验具名命令构造的请求类型与 rpc 注册表一致。
func TestFunctionsRegistryTypes(t *testing.T) {
	for method, sample := range map[string]proto.Message{
		serverv1.FunctionsService_CreateFunction_FullMethodName:   &serverv1.CreateFunctionRequest{},
		serverv1.FunctionsService_UpdateFunction_FullMethodName:   &serverv1.UpdateFunctionRequest{},
		serverv1.FunctionsService_CreateDeployment_FullMethodName: &serverv1.CreateDeploymentRequest{},
		serverv1.FunctionsService_SetVariables_FullMethodName:     &serverv1.SetVariablesRequest{},
		serverv1.FunctionsService_CreateExecution_FullMethodName:  &serverv1.CreateExecutionRequest{},
	} {
		e, err := lookupRPCMethod(method)
		if err != nil {
			t.Fatal(err)
		}
		if proto.MessageName(e.newReq()) != proto.MessageName(sample) {
			t.Errorf("注册表请求类型与具名命令不一致: %s", method)
		}
	}
}
