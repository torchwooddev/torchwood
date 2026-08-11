package main

import (
	"fmt"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"google.golang.org/protobuf/proto"
)

// rpcEntry 描述一个 unary 方法的请求/响应构造器。
type rpcEntry struct {
	newReq  func() proto.Message
	newResp func() proto.Message
}

// entry 由两个「样例」消息构造条目：每次调用 proto.Clone 产出全新实例，
// 避免共享可变状态；proto.Clone 对 Empty/ListRequest/Struct 等同样适用。
func entry(req, resp proto.Message) rpcEntry {
	return rpcEntry{
		newReq:  func() proto.Message { return proto.Clone(req) },
		newResp: func() proto.Message { return proto.Clone(resp) },
	}
}

// rpcRegistry 把完整 gRPC 方法名映射到请求/响应构造器，覆盖 proto/server/v1
// 全部方法（除 APIKeysService——API Key 凭证被拦截器禁止调用，见
// pkg/grpc/interceptor.IsAPIKeysServiceMethod）。供 rpc 逃生舱命令复用，
// 完整性由 TestRPCRegistryCoverage 保证；后续资源命令（databases/teams/storage/
// functions/oauth-providers）直接复用本表。
var rpcRegistry = map[string]rpcEntry{
	// HealthService（ACCESS_PUBLIC，公开）
	serverv1.HealthService_Check_FullMethodName:      entry(&serverv1.HealthCheckRequest{}, &serverv1.HealthCheckResponse{}),
	serverv1.HealthService_GetVersion_FullMethodName: entry(&serverv1.GetVersionRequest{}, &serverv1.GetVersionResponse{}),

	// ProjectsService（CLI 仅提供 list/get：CreateProject 限平台 admin，见设计文档 §4.1）
	serverv1.ProjectsService_CreateProject_FullMethodName: entry(&serverv1.CreateProjectRequest{}, &serverv1.Project{}),
	serverv1.ProjectsService_ListProjects_FullMethodName:  entry(&sharedv1.ListRequest{}, &serverv1.ListProjectsResponse{}),
	serverv1.ProjectsService_GetProject_FullMethodName:    entry(&serverv1.GetProjectRequest{}, &serverv1.Project{}),
	serverv1.ProjectsService_UpdateProject_FullMethodName: entry(&serverv1.UpdateProjectRequest{}, &serverv1.Project{}),

	// UsersService
	serverv1.UsersService_CreateUser_FullMethodName:         entry(&serverv1.CreateUserRequest{}, &serverv1.User{}),
	serverv1.UsersService_ListUsers_FullMethodName:          entry(&sharedv1.ListRequest{}, &serverv1.ListUsersResponse{}),
	serverv1.UsersService_GetUser_FullMethodName:            entry(&serverv1.GetUserRequest{}, &serverv1.User{}),
	serverv1.UsersService_UpdateUser_FullMethodName:         entry(&serverv1.UpdateUserRequest{}, &serverv1.User{}),
	serverv1.UsersService_UpdateUserPassword_FullMethodName: entry(&serverv1.UpdateUserPasswordRequest{}, &serverv1.User{}),
	serverv1.UsersService_DeleteUser_FullMethodName:         entry(&serverv1.GetUserRequest{}, &sharedv1.Empty{}),
	serverv1.UsersService_ListUserSessions_FullMethodName:   entry(&serverv1.GetUserRequest{}, &serverv1.ListUserSessionsResponse{}),
	serverv1.UsersService_DeleteUserSession_FullMethodName:  entry(&serverv1.DeleteUserSessionRequest{}, &sharedv1.Empty{}),
	serverv1.UsersService_CreateUserToken_FullMethodName:    entry(&serverv1.GetUserRequest{}, &serverv1.CreateUserTokenResponse{}),

	// DatabasesService
	serverv1.DatabasesService_CreateDatabase_FullMethodName:      entry(&serverv1.CreateDatabaseRequest{}, &serverv1.Database{}),
	serverv1.DatabasesService_ListDatabases_FullMethodName:       entry(&sharedv1.ListRequest{}, &serverv1.ListDatabasesResponse{}),
	serverv1.DatabasesService_GetDatabase_FullMethodName:         entry(&serverv1.GetDatabaseRequest{}, &serverv1.Database{}),
	serverv1.DatabasesService_DeleteDatabase_FullMethodName:      entry(&serverv1.GetDatabaseRequest{}, &sharedv1.Empty{}),
	serverv1.DatabasesService_CreateCollection_FullMethodName:    entry(&serverv1.CreateCollectionRequest{}, &serverv1.Collection{}),
	serverv1.DatabasesService_ListCollections_FullMethodName:     entry(&serverv1.ListCollectionsRequest{}, &serverv1.ListCollectionsResponse{}),
	serverv1.DatabasesService_GetCollection_FullMethodName:       entry(&serverv1.GetCollectionRequest{}, &serverv1.Collection{}),
	serverv1.DatabasesService_DeleteCollection_FullMethodName:    entry(&serverv1.GetCollectionRequest{}, &sharedv1.Empty{}),
	serverv1.DatabasesService_UpdateCollection_FullMethodName:    entry(&serverv1.UpdateCollectionRequest{}, &serverv1.Collection{}),
	serverv1.DatabasesService_CreateAttribute_FullMethodName:     entry(&serverv1.CreateAttributeRequest{}, &serverv1.Attribute{}),
	serverv1.DatabasesService_DeleteAttribute_FullMethodName:     entry(&serverv1.DeleteAttributeRequest{}, &sharedv1.Empty{}),
	serverv1.DatabasesService_CreateIndex_FullMethodName:         entry(&serverv1.CreateIndexRequest{}, &serverv1.Index{}),
	serverv1.DatabasesService_DeleteIndex_FullMethodName:         entry(&serverv1.DeleteIndexRequest{}, &sharedv1.Empty{}),
	serverv1.DatabasesService_CreateDocument_FullMethodName:      entry(&serverv1.CreateDocumentRequest{}, &serverv1.Document{}),
	serverv1.DatabasesService_ListDocuments_FullMethodName:       entry(&serverv1.ListDocumentsRequest{}, &serverv1.ListDocumentsResponse{}),
	serverv1.DatabasesService_GetDocument_FullMethodName:         entry(&serverv1.GetDocumentRequest{}, &serverv1.Document{}),
	serverv1.DatabasesService_UpdateDocument_FullMethodName:      entry(&serverv1.UpdateDocumentRequest{}, &serverv1.Document{}),
	serverv1.DatabasesService_UpsertDocument_FullMethodName:      entry(&serverv1.UpsertDocumentRequest{}, &serverv1.Document{}),
	serverv1.DatabasesService_DeleteDocument_FullMethodName:      entry(&serverv1.GetDocumentRequest{}, &sharedv1.Empty{}),
	serverv1.DatabasesService_CountDocuments_FullMethodName:      entry(&serverv1.ListDocumentsRequest{}, &serverv1.CountDocumentsResponse{}),
	serverv1.DatabasesService_BulkUpdateDocuments_FullMethodName: entry(&serverv1.BulkUpdateDocumentsRequest{}, &serverv1.BulkDocumentsResponse{}),
	serverv1.DatabasesService_BulkDeleteDocuments_FullMethodName: entry(&serverv1.BulkDeleteDocumentsRequest{}, &serverv1.BulkDocumentsResponse{}),

	// FunctionsService
	serverv1.FunctionsService_ListRuntimes_FullMethodName:       entry(&sharedv1.Empty{}, &serverv1.ListRuntimesResponse{}),
	serverv1.FunctionsService_ListSpecifications_FullMethodName: entry(&sharedv1.Empty{}, &serverv1.ListSpecificationsResponse{}),
	serverv1.FunctionsService_CreateFunction_FullMethodName:     entry(&serverv1.CreateFunctionRequest{}, &serverv1.Function{}),
	serverv1.FunctionsService_ListFunctions_FullMethodName:      entry(&sharedv1.ListRequest{}, &serverv1.ListFunctionsResponse{}),
	serverv1.FunctionsService_GetFunction_FullMethodName:        entry(&serverv1.GetFunctionRequest{}, &serverv1.Function{}),
	serverv1.FunctionsService_UpdateFunction_FullMethodName:     entry(&serverv1.UpdateFunctionRequest{}, &serverv1.Function{}),
	serverv1.FunctionsService_DeleteFunction_FullMethodName:     entry(&serverv1.GetFunctionRequest{}, &sharedv1.Empty{}),
	serverv1.FunctionsService_CreateDeployment_FullMethodName:   entry(&serverv1.CreateDeploymentRequest{}, &serverv1.Deployment{}),
	serverv1.FunctionsService_ListDeployments_FullMethodName:    entry(&serverv1.GetFunctionRequest{}, &serverv1.ListDeploymentsResponse{}),
	serverv1.FunctionsService_GetDeployment_FullMethodName:      entry(&serverv1.GetDeploymentRequest{}, &serverv1.Deployment{}),
	serverv1.FunctionsService_DeleteDeployment_FullMethodName:   entry(&serverv1.GetDeploymentRequest{}, &sharedv1.Empty{}),
	serverv1.FunctionsService_SetVariables_FullMethodName:       entry(&serverv1.SetVariablesRequest{}, &serverv1.Variables{}),
	serverv1.FunctionsService_GetVariables_FullMethodName:       entry(&serverv1.GetFunctionRequest{}, &serverv1.Variables{}),
	serverv1.FunctionsService_CreateExecution_FullMethodName:    entry(&serverv1.CreateExecutionRequest{}, &serverv1.Execution{}),
	serverv1.FunctionsService_ListExecutions_FullMethodName:     entry(&serverv1.GetFunctionRequest{}, &serverv1.ListExecutionsResponse{}),
	serverv1.FunctionsService_GetExecution_FullMethodName:       entry(&serverv1.GetExecutionRequest{}, &serverv1.Execution{}),

	// OAuthProvidersService
	serverv1.OAuthProvidersService_ListOAuthProviders_FullMethodName:  entry(&sharedv1.ListRequest{}, &serverv1.ListOAuthProvidersResponse{}),
	serverv1.OAuthProvidersService_UpsertOAuthProvider_FullMethodName: entry(&serverv1.UpsertOAuthProviderRequest{}, &serverv1.OAuthProvider{}),
	serverv1.OAuthProvidersService_DeleteOAuthProvider_FullMethodName: entry(&serverv1.DeleteOAuthProviderRequest{}, &sharedv1.Empty{}),

	// StorageService（文件上传/下载走独立 HTTP handler，gRPC 面仅元数据操作）
	serverv1.StorageService_CreateBucket_FullMethodName:    entry(&serverv1.CreateBucketRequest{}, &serverv1.Bucket{}),
	serverv1.StorageService_ListBuckets_FullMethodName:     entry(&sharedv1.ListRequest{}, &serverv1.ListBucketsResponse{}),
	serverv1.StorageService_GetBucket_FullMethodName:       entry(&serverv1.GetBucketRequest{}, &serverv1.Bucket{}),
	serverv1.StorageService_DeleteBucket_FullMethodName:    entry(&serverv1.GetBucketRequest{}, &sharedv1.Empty{}),
	serverv1.StorageService_UpdateBucket_FullMethodName:    entry(&serverv1.UpdateBucketRequest{}, &serverv1.Bucket{}),
	serverv1.StorageService_CreateFile_FullMethodName:      entry(&serverv1.CreateFileRequest{}, &serverv1.File{}),
	serverv1.StorageService_ListFiles_FullMethodName:       entry(&serverv1.ListFilesRequest{}, &serverv1.ListFilesResponse{}),
	serverv1.StorageService_GetFile_FullMethodName:         entry(&serverv1.GetFileRequest{}, &serverv1.File{}),
	serverv1.StorageService_DeleteFile_FullMethodName:      entry(&serverv1.GetFileRequest{}, &sharedv1.Empty{}),
	serverv1.StorageService_UpdateFile_FullMethodName:      entry(&serverv1.UpdateFileRequest{}, &serverv1.File{}),
	serverv1.StorageService_CreateFileToken_FullMethodName: entry(&serverv1.CreateFileTokenRequest{}, &serverv1.FileToken{}),
	serverv1.StorageService_GetStorageUsage_FullMethodName: entry(&serverv1.GetStorageUsageRequest{}, &serverv1.StorageUsage{}),

	// TeamsService
	serverv1.TeamsService_CreateTeam_FullMethodName:             entry(&serverv1.CreateTeamRequest{}, &serverv1.Team{}),
	serverv1.TeamsService_ListTeams_FullMethodName:              entry(&sharedv1.ListRequest{}, &serverv1.ListTeamsResponse{}),
	serverv1.TeamsService_GetTeam_FullMethodName:                entry(&serverv1.GetTeamRequest{}, &serverv1.Team{}),
	serverv1.TeamsService_DeleteTeam_FullMethodName:             entry(&serverv1.GetTeamRequest{}, &sharedv1.Empty{}),
	serverv1.TeamsService_GetTeamPrefs_FullMethodName:           entry(&serverv1.GetTeamRequest{}, &serverv1.GetTeamPrefsResponse{}),
	serverv1.TeamsService_UpdateTeamPrefs_FullMethodName:        entry(&serverv1.UpdateTeamPrefsRequest{}, &serverv1.GetTeamPrefsResponse{}),
	serverv1.TeamsService_CreateMembership_FullMethodName:       entry(&serverv1.CreateMembershipRequest{}, &serverv1.Membership{}),
	serverv1.TeamsService_ListMemberships_FullMethodName:        entry(&serverv1.ListMembershipsRequest{}, &serverv1.ListMembershipsResponse{}),
	serverv1.TeamsService_GetMembership_FullMethodName:          entry(&serverv1.GetMembershipRequest{}, &serverv1.Membership{}),
	serverv1.TeamsService_UpdateMembership_FullMethodName:       entry(&serverv1.UpdateMembershipRequest{}, &serverv1.Membership{}),
	serverv1.TeamsService_UpdateMembershipStatus_FullMethodName: entry(&serverv1.UpdateMembershipStatusRequest{}, &serverv1.Membership{}),
	serverv1.TeamsService_DeleteMembership_FullMethodName:       entry(&serverv1.GetMembershipRequest{}, &sharedv1.Empty{}),
}

// lookupRPCMethod 从注册表解析完整方法名；未注册时返回清晰错误。
func lookupRPCMethod(method string) (rpcEntry, error) {
	e, ok := rpcRegistry[method]
	if !ok {
		return rpcEntry{}, fmt.Errorf("未知方法 %q：请使用完整 gRPC 方法名（如 /torchwood.server.v1.UsersService/ListUsers）", method)
	}
	return e, nil
}
