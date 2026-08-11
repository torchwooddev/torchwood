package main

import (
	"strings"
	"testing"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"google.golang.org/protobuf/proto"
)

func TestBuildCreateBucketReq(t *testing.T) {
	tests := []struct {
		name        string
		bucketName  string
		permissions string
		public      bool
		wantErr     string
	}{
		{name: "缺 name", wantErr: "--name 必填"},
		{name: "最小字段", bucketName: "assets", wantErr: ""},
		{name: "公开桶", bucketName: "assets", public: true, permissions: `["read(\"all\")"]`, wantErr: ""},
		{name: "permissions 非法", bucketName: "assets", permissions: `x`, wantErr: "--permissions 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateBucketReq(tt.bucketName, tt.permissions, tt.public)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetName() != tt.bucketName || req.GetPublic() != tt.public {
				t.Errorf("请求不匹配: %v", req)
			}
			if tt.permissions != "" && len(req.GetPermissions()) != 1 {
				t.Errorf("permissions 未合并: %v", req.GetPermissions())
			}
		})
	}
}

func TestBuildUpdateBucketReq(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		newName string
		public  *bool
		wantErr string
	}{
		{name: "缺 id", wantErr: "缺少存储桶 ID"},
		{name: "仅改名", id: "b1", newName: "new", wantErr: ""},
		{name: "仅 public", id: "b1", public: boolPtr(true), wantErr: ""},
		{name: "全字段", id: "b1", newName: "new", public: boolPtr(false), wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildUpdateBucketReq(tt.id, tt.newName, tt.public)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.newName == "" && req.GetName() != "" {
				t.Errorf("未传 --name 不应设置: %q", req.GetName())
			}
			if tt.newName != "" && (req.Name == nil || *req.Name != tt.newName) {
				t.Errorf("name 不匹配: %v", req.Name)
			}
			if tt.public == nil {
				if req.Public != nil {
					t.Errorf("public 不应设置: %v", req.Public)
				}
			} else if req.Public == nil || *req.Public != *tt.public {
				t.Errorf("public 不匹配: %v", req.Public)
			}
		})
	}
}

func TestBuildUpdateFileReq(t *testing.T) {
	tests := []struct {
		name     string
		bucketID string
		fileID   string
		fileName string
		mimeType string
		metadata string
		wantErr  string
	}{
		{name: "无可更新字段", bucketID: "b1", fileID: "f1", wantErr: "--name/--mime-type/--metadata 至少提供一个"},
		{name: "仅改名", bucketID: "b1", fileID: "f1", fileName: "new.png", wantErr: ""},
		{name: "仅 metadata", bucketID: "b1", fileID: "f1", metadata: `{"author":"x"}`, wantErr: ""},
		{name: "全字段", bucketID: "b1", fileID: "f1", fileName: "new.png", mimeType: "image/png",
			metadata: `{"author":"x"}`, wantErr: ""},
		{name: "metadata 非法", bucketID: "b1", fileID: "f1", metadata: `[1]`, wantErr: "--metadata 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildUpdateFileReq(tt.bucketID, tt.fileID, tt.fileName, tt.mimeType, tt.metadata)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.fileName == "" && req.GetName() != "" {
				t.Errorf("未传 --name 不应设置: %q", req.GetName())
			}
			if tt.fileName != "" && (req.Name == nil || *req.Name != tt.fileName) {
				t.Errorf("name 不匹配: %v", req.Name)
			}
			if tt.metadata != "" && req.GetMetadata()["author"] != "x" {
				t.Errorf("metadata 未解析: %v", req.GetMetadata())
			}
		})
	}
}

// TestStorageRegistryTypes 校验具名命令构造的请求类型与 rpc 注册表一致。
func TestStorageRegistryTypes(t *testing.T) {
	for method, sample := range map[string]proto.Message{
		serverv1.StorageService_CreateBucket_FullMethodName: &serverv1.CreateBucketRequest{},
		serverv1.StorageService_UpdateBucket_FullMethodName: &serverv1.UpdateBucketRequest{},
		serverv1.StorageService_UpdateFile_FullMethodName:   &serverv1.UpdateFileRequest{},
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
