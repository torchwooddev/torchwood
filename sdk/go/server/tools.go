package server

import (
	"context"
	"fmt"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
)

// Agent 默认工具名（E-7 overlay）。完整产品 API 仍是 201 个 RPC。
const (
	ToolListUsers       = "list_users"
	ToolGetUser         = "get_user"
	ToolCreateUser      = "create_user"
	ToolQueryDocuments  = "query_documents"
	ToolGetDocument     = "get_document"
	ToolCreateDocument  = "create_document"
	ToolUpdateDocument  = "update_document"
	ToolUpsertDocument  = "upsert_document"
	ToolDeleteDocument  = "delete_document"
	ToolListCollections = "list_collections"
	ToolGetCollection   = "get_collection"
	ToolInvokeFunction  = "invoke_function"
	ToolListFiles       = "list_files"
	ToolGetFile         = "get_file"
	ToolGrantAsset      = "grant_asset"
	ToolListUserAssets  = "list_user_assets"
	ToolGetOrder        = "get_order"
	ToolGetHealth       = "get_health"
)

// Tool 将 Agent 工具名映射到已有 Server unary（不新增 gRPC 服务）。
// 不含 API key 管理；InvokeJSON 仍排除 APIKeysService。
type Tool struct {
	Name       string
	FullMethod string
	InputNotes string
}

// Tools 是锁定的 18 个默认工具，顺序与 docs/review/wave3-e7-tool-catalog.md 一致。
// catalog 只读：不要原地改写切片或元素；LookupTool 使用 init 时的值拷贝。
var Tools = []Tool{
	{
		Name:       ToolListUsers,
		FullMethod: serverv1.UsersService_ListUsers_FullMethodName,
		InputNotes: "shared.v1.ListRequest：page_size / page_token / queries[]string",
	},
	{
		Name:       ToolGetUser,
		FullMethod: serverv1.UsersService_GetUser_FullMethodName,
		InputNotes: "id",
	},
	{
		Name:       ToolCreateUser,
		FullMethod: serverv1.UsersService_CreateUser_FullMethodName,
		InputNotes: "email, password；可选 name / status / labels / prefs",
	},
	{
		Name:       ToolQueryDocuments,
		FullMethod: serverv1.DatabasesService_ListDocuments_FullMethodName,
		InputNotes: "必填 database_id、collection_id。优先 query（shared.v1.Query AST：filter/orders/page_size/page_token）；仍接受 queries[]string + page_size/page_token。两者同时提供且冲突 → InvalidArgument",
	},
	{
		Name:       ToolGetDocument,
		FullMethod: serverv1.DatabasesService_GetDocument_FullMethodName,
		InputNotes: "database_id, collection_id, document_id",
	},
	{
		Name:       ToolCreateDocument,
		FullMethod: serverv1.DatabasesService_CreateDocument_FullMethodName,
		InputNotes: "database_id, collection_id, document_id, data；可选 permissions",
	},
	{
		Name:       ToolUpdateDocument,
		FullMethod: serverv1.DatabasesService_UpdateDocument_FullMethodName,
		InputNotes: "database_id, collection_id, document_id；可选 data / permissions / increment。用户集合须带 version（OCC）",
	},
	{
		Name:       ToolUpsertDocument,
		FullMethod: serverv1.DatabasesService_UpsertDocument_FullMethodName,
		InputNotes: "database_id, collection_id, document_id, data；可选 permissions、conflict_columns",
	},
	{
		Name:       ToolDeleteDocument,
		FullMethod: serverv1.DatabasesService_DeleteDocument_FullMethodName,
		InputNotes: "database_id, collection_id, document_id。用户集合须带 version（OCC）",
	},
	{
		Name:       ToolListCollections,
		FullMethod: serverv1.DatabasesService_ListCollections_FullMethodName,
		InputNotes: "database_id；可选 queries[]string、page_size、page_token",
	},
	{
		Name:       ToolGetCollection,
		FullMethod: serverv1.DatabasesService_GetCollection_FullMethodName,
		InputNotes: "database_id, collection_id",
	},
	{
		Name:       ToolInvokeFunction,
		FullMethod: serverv1.FunctionsService_CreateExecution_FullMethodName,
		InputNotes: "function_id；可选 deployment_id、data、async",
	},
	{
		Name:       ToolListFiles,
		FullMethod: serverv1.StorageService_ListFiles_FullMethodName,
		InputNotes: "bucket_id；可选 queries[]string、page_size、page_token",
	},
	{
		Name:       ToolGetFile,
		FullMethod: serverv1.StorageService_GetFile_FullMethodName,
		InputNotes: "bucket_id, file_id",
	},
	{
		Name:       ToolGrantAsset,
		FullMethod: serverv1.AssetsService_Grant_FullMethodName,
		InputNotes: "owner_id, def_code, quantity, idempotency_key；可选 expires_at / level / metadata / ref_type / ref_id",
	},
	{
		Name:       ToolListUserAssets,
		FullMethod: serverv1.AssetsService_ListUserAssets_FullMethodName,
		InputNotes: "owner_id；可选 page_size、page_token",
	},
	{
		Name:       ToolGetOrder,
		FullMethod: serverv1.PaymentsService_GetOrder_FullMethodName,
		InputNotes: "order_id",
	},
	{
		Name:       ToolGetHealth,
		FullMethod: serverv1.HealthService_Check_FullMethodName,
		InputNotes: "无入参",
	},
}

var toolsByName map[string]Tool

func init() {
	toolsByName = make(map[string]Tool, len(Tools))
	for _, t := range Tools {
		toolsByName[t.Name] = t
	}
}

// LookupTool 按工具名查找 catalog 条目。
func LookupTool(name string) (Tool, bool) {
	t, ok := toolsByName[name]
	return t, ok
}

// InvokeTool 按 catalog 工具名调用对应 Server unary（走 InvokeJSON）。
func (c *Client) InvokeTool(ctx context.Context, name string, reqJSON []byte) ([]byte, error) {
	t, ok := LookupTool(name)
	if !ok {
		return nil, fmt.Errorf("torchwood: unknown tool %q", name)
	}
	return c.InvokeJSON(ctx, t.FullMethod, reqJSON)
}
