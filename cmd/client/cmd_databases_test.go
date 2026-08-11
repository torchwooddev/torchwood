package main

import (
	"strings"
	"testing"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"google.golang.org/protobuf/proto"
)

func TestBuildCreateDatabaseReq(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		label   string
		wantErr string
	}{
		{name: "缺 id", wantErr: "--id 必填"},
		{name: "缺 name", id: "app", wantErr: "--name 必填"},
		{name: "全字段", id: "app", label: "应用库", wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateDatabaseReq(tt.id, tt.label)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetId() != tt.id || req.GetName() != tt.label {
				t.Errorf("请求不匹配: %v", req)
			}
		})
	}
}

func TestBuildCreateCollectionReq(t *testing.T) {
	tests := []struct {
		name             string
		databaseID       string
		id               string
		collectionName   string
		permissions      string
		documentSecurity bool
		wantErr          string
	}{
		{name: "缺 database-id", wantErr: "缺少 database-id"},
		{name: "缺 id", databaseID: "app", wantErr: "--id 必填"},
		{name: "缺 name", databaseID: "app", id: "notes", wantErr: "--name 必填"},
		{name: "最小字段", databaseID: "app", id: "notes", collectionName: "笔记", wantErr: ""},
		{name: "权限与安全", databaseID: "app", id: "notes", collectionName: "笔记",
			permissions: `["read(\"users\")"]`, documentSecurity: true, wantErr: ""},
		{name: "permissions 非法 JSON", databaseID: "app", id: "notes", collectionName: "笔记",
			permissions: `[not-json`, wantErr: "--permissions 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateCollectionReq(tt.databaseID, tt.id, tt.collectionName, tt.permissions, tt.documentSecurity)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetId() != tt.id || req.GetName() != tt.collectionName || req.GetDatabaseId() != tt.databaseID {
				t.Errorf("请求不匹配: %v", req)
			}
			if tt.documentSecurity && req.GetDocumentSecurity() != true {
				t.Errorf("documentSecurity 未设置: %v", req.GetDocumentSecurity())
			}
			if tt.permissions != "" && len(req.GetPermissions()) != 1 {
				t.Errorf("permissions 未合并: %v", req.GetPermissions())
			}
		})
	}
}

func TestBuildUpdateCollectionReq(t *testing.T) {
	tests := []struct {
		name             string
		databaseID       string
		collectionID     string
		collectionName   string
		permissions      string
		documentSecurity *bool
		disabled         *bool
		wantErr          string
		wantPermissions  bool
		wantDocSec       *bool
		wantDisabled     *bool
	}{
		{name: "缺 database-id", wantErr: "缺少 database-id"},
		{name: "仅 id", databaseID: "app", collectionID: "c1", wantErr: ""},
		{name: "改名", databaseID: "app", collectionID: "c1", collectionName: "新名", wantErr: ""},
		{name: "权限替换", databaseID: "app", collectionID: "c1", permissions: `["read(\"all\")"]`,
			wantErr: "", wantPermissions: true},
		{name: "optional bool", databaseID: "app", collectionID: "c1",
			documentSecurity: boolPtr(false), disabled: boolPtr(true),
			wantDocSec: boolPtr(false), wantDisabled: boolPtr(true), wantErr: ""},
		{name: "permissions 非法", databaseID: "app", collectionID: "c1",
			permissions: `nope`, wantErr: "--permissions 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildUpdateCollectionReq(tt.databaseID, tt.collectionID, tt.collectionName,
				tt.permissions, tt.documentSecurity, tt.disabled)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.collectionName == "" && req.GetName() != "" {
				t.Errorf("未传 --name 不应设置: %q", req.GetName())
			}
			if tt.collectionName != "" && req.GetName() != tt.collectionName {
				t.Errorf("name 不匹配: %q", req.GetName())
			}
			if tt.wantPermissions != (req.Permissions != nil) {
				t.Errorf("permissions presence 不匹配: %v", req.Permissions)
			}
			if tt.wantDocSec == nil {
				if req.DocumentSecurity != nil {
					t.Errorf("documentSecurity 不应设置: %v", req.DocumentSecurity)
				}
			} else if req.DocumentSecurity == nil || *req.DocumentSecurity != *tt.wantDocSec {
				t.Errorf("documentSecurity 不匹配: %v", req.DocumentSecurity)
			}
			if tt.wantDisabled == nil {
				if req.Disabled != nil {
					t.Errorf("disabled 不应设置: %v", req.Disabled)
				}
			} else if req.Disabled == nil || *req.Disabled != *tt.wantDisabled {
				t.Errorf("disabled 不匹配: %v", req.Disabled)
			}
		})
	}
}

func TestBuildCreateAttributeReq(t *testing.T) {
	tests := []struct {
		name         string
		databaseID   string
		collectionID string
		key          string
		typ          string
		size         int32
		required     bool
		array        bool
		defaultValue string
		wantErr      string
	}{
		{name: "缺 key", databaseID: "app", collectionID: "c1", typ: "string", wantErr: "--key 必填"},
		{name: "缺 type", databaseID: "app", collectionID: "c1", key: "title", wantErr: "--type 必填"},
		{name: "最小字段", databaseID: "app", collectionID: "c1", key: "title", typ: "string", wantErr: ""},
		{name: "全字段", databaseID: "app", collectionID: "c1", key: "tags", typ: "string",
			size: 64, required: true, array: true, defaultValue: `["a"]`, wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateAttributeReq(tt.databaseID, tt.collectionID, tt.key, tt.typ,
				tt.size, tt.required, tt.array, tt.defaultValue)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetKey() != tt.key || req.GetType() != tt.typ || req.GetSize() != tt.size ||
				req.GetRequired() != tt.required || req.GetArray() != tt.array || req.GetDefaultValue() != tt.defaultValue {
				t.Errorf("请求不匹配: %v", req)
			}
		})
	}
}

func TestBuildCreateIndexReq(t *testing.T) {
	tests := []struct {
		name         string
		databaseID   string
		collectionID string
		id           string
		typ          string
		attributes   string
		orders       string
		wantErr      string
	}{
		{name: "缺 id", databaseID: "app", collectionID: "c1", typ: "key", attributes: `["title"]`, wantErr: "--id 必填"},
		{name: "缺 type", databaseID: "app", collectionID: "c1", id: "ix1", attributes: `["title"]`, wantErr: "--type 必填"},
		{name: "缺 attributes", databaseID: "app", collectionID: "c1", id: "ix1", typ: "key", wantErr: "--attributes 必填"},
		{name: "最小字段", databaseID: "app", collectionID: "c1", id: "ix1", typ: "key",
			attributes: `["title"]`, wantErr: ""},
		{name: "全字段", databaseID: "app", collectionID: "c1", id: "ix1", typ: "unique",
			attributes: `["title","author"]`, orders: `["asc","desc"]`, wantErr: ""},
		{name: "orders 非法", databaseID: "app", collectionID: "c1", id: "ix1", typ: "key",
			attributes: `["title"]`, orders: `oops`, wantErr: "--orders 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateIndexReq(tt.databaseID, tt.collectionID, tt.id, tt.typ, tt.attributes, tt.orders)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetId() != tt.id || req.GetType() != tt.typ || len(req.GetAttributes()) < 1 {
				t.Errorf("请求不匹配: %v", req)
			}
			if tt.orders != "" && len(req.GetOrders()) != 2 {
				t.Errorf("orders 未解析: %v", req.GetOrders())
			}
		})
	}
}

func TestBuildCreateDocumentReq(t *testing.T) {
	tests := []struct {
		name         string
		databaseID   string
		collectionID string
		documentID   string
		data         string
		permissions  string
		wantErr      string
		wantTitle    string
	}{
		{name: "缺 data", databaseID: "app", collectionID: "c1", wantErr: "--data 必填"},
		{name: "data 非对象", databaseID: "app", collectionID: "c1", data: `[1,2]`, wantErr: "--data 解析失败"},
		{name: "data 非法 JSON", databaseID: "app", collectionID: "c1", data: `{`, wantErr: "--data 解析失败"},
		{name: "最小字段", databaseID: "app", collectionID: "c1", data: `{"title":"hi"}`, wantErr: "",
			wantTitle: "hi"},
		{name: "指定 id 与权限", databaseID: "app", collectionID: "c1", documentID: "doc1",
			data: `{"title":"hi"}`, permissions: `["read(\"all\")"]`, wantErr: "", wantTitle: "hi"},
		{name: "permissions 非法", databaseID: "app", collectionID: "c1", data: `{"title":"hi"}`,
			permissions: `x`, wantErr: "--permissions 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildCreateDocumentReq(tt.databaseID, tt.collectionID, tt.documentID, tt.data, tt.permissions)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.GetDocumentId() != tt.documentID {
				t.Errorf("documentId 不匹配: %q", req.GetDocumentId())
			}
			if req.GetData() == nil || req.GetData().AsMap()["title"] != tt.wantTitle {
				t.Errorf("data 未正确解析: %v", req.GetData())
			}
			if tt.permissions != "" && len(req.GetPermissions()) != 1 {
				t.Errorf("permissions 未合并: %v", req.GetPermissions())
			}
		})
	}
}

func TestBuildUpdateDocumentReq(t *testing.T) {
	tests := []struct {
		name         string
		databaseID   string
		collectionID string
		documentID   string
		data         string
		permissions  string
		increment    string
		wantErr      string
	}{
		{name: "无可更新字段", databaseID: "app", collectionID: "c1", documentID: "d1",
			wantErr: "--data/--permissions/--increment 至少提供一个"},
		{name: "仅 data", databaseID: "app", collectionID: "c1", documentID: "d1",
			data: `{"title":"new"}`, wantErr: ""},
		{name: "仅 increment", databaseID: "app", collectionID: "c1", documentID: "d1",
			increment: `{"views":1}`, wantErr: ""},
		{name: "全字段", databaseID: "app", collectionID: "c1", documentID: "d1",
			data: `{"title":"new"}`, permissions: `["read(\"all\")"]`, increment: `{"views":1}`, wantErr: ""},
		{name: "increment 非法", databaseID: "app", collectionID: "c1", documentID: "d1",
			increment: `{"views":"x"}`, wantErr: "--increment 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildUpdateDocumentReq(tt.databaseID, tt.collectionID, tt.documentID,
				tt.data, tt.permissions, tt.increment)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.data != "" && (req.GetData() == nil || req.GetData().AsMap()["title"] != "new") {
				t.Errorf("data 未解析: %v", req.GetData())
			}
			if tt.increment != "" && req.GetIncrement()["views"] != 1 {
				t.Errorf("increment 未解析: %v", req.GetIncrement())
			}
		})
	}
}

func TestBuildUpsertDocumentReq(t *testing.T) {
	tests := []struct {
		name            string
		databaseID      string
		collectionID    string
		documentID      string
		data            string
		permissions     string
		conflictColumns string
		wantErr         string
	}{
		{name: "缺 data", databaseID: "app", collectionID: "c1", documentID: "d1",
			conflictColumns: `["email"]`, wantErr: "--data 必填"},
		{name: "缺 conflict-columns", databaseID: "app", collectionID: "c1", documentID: "d1",
			data: `{"email":"a@b.c"}`, wantErr: "--conflict-columns 必填"},
		{name: "全字段", databaseID: "app", collectionID: "c1", documentID: "d1",
			data: `{"email":"a@b.c"}`, permissions: `["read(\"all\")"]`,
			conflictColumns: `["email"]`, wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildUpsertDocumentReq(tt.databaseID, tt.collectionID, tt.documentID,
				tt.data, tt.permissions, tt.conflictColumns)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(req.GetConflictColumns()) != 1 || req.GetConflictColumns()[0] != "email" {
				t.Errorf("conflictColumns 未解析: %v", req.GetConflictColumns())
			}
			if req.GetData() == nil || req.GetData().AsMap()["email"] != "a@b.c" {
				t.Errorf("data 未解析: %v", req.GetData())
			}
		})
	}
}

func TestBuildBulkDocumentsReq(t *testing.T) {
	tests := []struct {
		name         string
		databaseID   string
		collectionID string
		documentIDs  string
		data         string
		permissions  string
		bulkDelete   bool
		wantErr      string
	}{
		{name: "缺 document-ids", databaseID: "app", collectionID: "c1", data: `{}`, wantErr: "--document-ids 必填"},
		{name: "bulk-update 缺 data", databaseID: "app", collectionID: "c1",
			documentIDs: `["d1","d2"]`, wantErr: "--data 必填"},
		{name: "bulk-update 全字段", databaseID: "app", collectionID: "c1",
			documentIDs: `["d1","d2"]`, data: `{"status":"x"}`, permissions: `["read(\"all\")"]`, wantErr: ""},
		{name: "bulk-delete", databaseID: "app", collectionID: "c1",
			documentIDs: `["d1","d2"]`, bulkDelete: true, wantErr: ""},
		{name: "document-ids 非法", databaseID: "app", collectionID: "c1",
			documentIDs: `d1`, data: `{}`, wantErr: "--document-ids 解析失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ids []string
			var err error
			if tt.bulkDelete {
				var req *serverv1.BulkDeleteDocumentsRequest
				req, err = buildBulkDeleteDocumentsReq(tt.databaseID, tt.collectionID, tt.documentIDs)
				if err == nil && req != nil {
					ids = req.GetDocumentIds()
				}
			} else {
				var req *serverv1.BulkUpdateDocumentsRequest
				req, err = buildBulkUpdateDocumentsReq(tt.databaseID, tt.collectionID, tt.documentIDs, tt.data, tt.permissions)
				if err == nil && req != nil {
					ids = req.GetDocumentIds()
				}
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(ids) != 2 {
				t.Errorf("documentIds 未解析: %v", ids)
			}
		})
	}
}

// TestDatabasesRegistryTypes 校验具名命令构造的请求类型与 rpc 注册表一致。
func TestDatabasesRegistryTypes(t *testing.T) {
	for method, sample := range map[string]proto.Message{
		serverv1.DatabasesService_CreateDatabase_FullMethodName:      &serverv1.CreateDatabaseRequest{},
		serverv1.DatabasesService_CreateCollection_FullMethodName:    &serverv1.CreateCollectionRequest{},
		serverv1.DatabasesService_UpdateCollection_FullMethodName:    &serverv1.UpdateCollectionRequest{},
		serverv1.DatabasesService_CreateAttribute_FullMethodName:     &serverv1.CreateAttributeRequest{},
		serverv1.DatabasesService_CreateIndex_FullMethodName:         &serverv1.CreateIndexRequest{},
		serverv1.DatabasesService_CreateDocument_FullMethodName:      &serverv1.CreateDocumentRequest{},
		serverv1.DatabasesService_UpdateDocument_FullMethodName:      &serverv1.UpdateDocumentRequest{},
		serverv1.DatabasesService_UpsertDocument_FullMethodName:      &serverv1.UpsertDocumentRequest{},
		serverv1.DatabasesService_BulkUpdateDocuments_FullMethodName: &serverv1.BulkUpdateDocumentsRequest{},
		serverv1.DatabasesService_BulkDeleteDocuments_FullMethodName: &serverv1.BulkDeleteDocumentsRequest{},
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
