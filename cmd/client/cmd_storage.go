package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
)

// newStorageCmd 覆盖 StorageService 元数据操作：buckets（create/list/get/
// update/delete）、files（list/get/update/delete）、usage。
// 不做文件上传/下载（独立 HTTP handler）与分片上传会话；CreateFile（bytes
// 上传）与 CreateFileToken 亦不提供（token 用途为 HTTP 下载签名）。
func newStorageCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "存储管理（StorageService 元数据操作；上传/下载走独立 HTTP handler，CLI 不提供）",
	}
	cmd.AddCommand(
		newStorageBucketsCmd(g),
		newStorageFilesCmd(g),
		newStorageUsageCmd(g),
	)
	return cmd
}

// newStorageBucketsCmd: storage buckets create/list/get/update/delete。
func newStorageBucketsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "buckets",
		Short: "存储桶管理",
	}
	cmd.AddCommand(
		newStorageBucketsCreateCmd(g),
		newStorageBucketsListCmd(g),
		newStorageBucketsGetCmd(g),
		newStorageBucketsUpdateCmd(g),
		newStorageBucketsDeleteCmd(g),
	)
	return cmd
}

func newStorageBucketsCreateCmd(g *globalFlags) *cobra.Command {
	var name, permissions string
	var public bool
	cmd := &cobra.Command{
		Use:   "create --name <name>",
		Short: "创建存储桶",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateBucketReq(name, permissions, public)
			if err != nil {
				return err
			}
			resp := &serverv1.Bucket{}
			if err := invoke(g, serverv1.StorageService_CreateBucket_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "存储桶名称（必填）")
	cmd.Flags().StringVar(&permissions, "permissions", "", "权限 JSON 数组")
	cmd.Flags().BoolVar(&public, "public", false, "是否公开可读")
	return cmd
}

func newStorageBucketsListCmd(g *globalFlags) *cobra.Command {
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出存储桶",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.ListBucketsResponse{}
			if err := invoke(g, serverv1.StorageService_ListBuckets_FullMethodName, buildListRequest(pageSize, pageToken), resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().Int32Var(&pageSize, "page-size", 0, "每页条数（服务端默认 50，上限 1000）")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "上一页返回的 next_page_token")
	return cmd
}

func newStorageBucketsGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "按 ID 获取存储桶",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.Bucket{}
			if err := invoke(g, serverv1.StorageService_GetBucket_FullMethodName, &serverv1.GetBucketRequest{Id: args[0]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newStorageBucketsUpdateCmd(g *globalFlags) *cobra.Command {
	var name string
	var public bool
	cmd := &cobra.Command{
		Use:   "update <id> [--name] [--public]",
		Short: "更新存储桶（仅更新显式传入的字段）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateBucketReq(args[0], name, changedBoolPtr(cmd, "public", public))
			if err != nil {
				return err
			}
			resp := &serverv1.Bucket{}
			if err := invoke(g, serverv1.StorageService_UpdateBucket_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "新名称")
	cmd.Flags().BoolVar(&public, "public", false, "公开可读开关（显式传 --public=true/false 才生效）")
	return cmd
}

func newStorageBucketsDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "删除存储桶",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.Bucket{}
			if err := invoke(g, serverv1.StorageService_DeleteBucket_FullMethodName, &serverv1.GetBucketRequest{Id: args[0]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// newStorageFilesCmd: storage files list/get/update/delete。
func newStorageFilesCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "files",
		Short: "文件元数据管理（上传/下载/分片会话走 HTTP，CLI 不提供）",
	}
	cmd.AddCommand(
		newStorageFilesListCmd(g),
		newStorageFilesGetCmd(g),
		newStorageFilesUpdateCmd(g),
		newStorageFilesDeleteCmd(g),
	)
	return cmd
}

func newStorageFilesListCmd(g *globalFlags) *cobra.Command {
	var queries string
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list <bucket-id>",
		Short: "列出桶内文件",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildListFilesReq(args[0], queries, pageSize, pageToken)
			if err != nil {
				return err
			}
			resp := &serverv1.ListFilesResponse{}
			if err := invoke(g, serverv1.StorageService_ListFiles_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&queries, "queries", "", "Appwrite 风格查询 JSON 数组")
	cmd.Flags().Int32Var(&pageSize, "page-size", 0, "每页条数（服务端默认 50，上限 1000）")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "上一页返回的 next_page_token")
	return cmd
}

func newStorageFilesGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <bucket-id> <file-id>",
		Short: "按 ID 获取文件元数据",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.File{}
			if err := invoke(g, serverv1.StorageService_GetFile_FullMethodName, &serverv1.GetFileRequest{BucketId: args[0], FileId: args[1]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newStorageFilesUpdateCmd(g *globalFlags) *cobra.Command {
	var name, mimeType, metadata string
	cmd := &cobra.Command{
		Use:   "update <bucket-id> <file-id> [--name] [--mime-type] [--metadata]",
		Short: "更新文件元数据（仅更新显式传入的字段）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateFileReq(args[0], args[1], name, mimeType, metadata)
			if err != nil {
				return err
			}
			resp := &serverv1.File{}
			if err := invoke(g, serverv1.StorageService_UpdateFile_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "新文件名")
	cmd.Flags().StringVar(&mimeType, "mime-type", "", "新 MIME 类型")
	cmd.Flags().StringVar(&metadata, "metadata", "", "元数据 JSON 对象（如 '{\"author\":\"x\"}'）")
	return cmd
}

func newStorageFilesDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <bucket-id> <file-id>",
		Short: "删除文件",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.File{}
			if err := invoke(g, serverv1.StorageService_DeleteFile_FullMethodName, &serverv1.GetFileRequest{BucketId: args[0], FileId: args[1]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newStorageUsageCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "获取存储用量（桶数/文件数/总大小）",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.StorageUsage{}
			if err := invoke(g, serverv1.StorageService_GetStorageUsage_FullMethodName, &serverv1.GetStorageUsageRequest{}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// buildCreateBucketReq 构造 CreateBucketRequest（name 必填）。
func buildCreateBucketReq(name, permissions string, public bool) (*serverv1.CreateBucketRequest, error) {
	if name == "" {
		return nil, fmt.Errorf("--name 必填")
	}
	perms, err := jsonStringList(permissions, "permissions")
	if err != nil {
		return nil, err
	}
	return &serverv1.CreateBucketRequest{Name: name, Permissions: perms, Public: public}, nil
}

// buildUpdateBucketReq 构造 UpdateBucketRequest：仅设置显式传入的字段；
// name 非空即设置（清空请用 --data 全量合并），public 依赖 presence。
func buildUpdateBucketReq(id, name string, public *bool) (*serverv1.UpdateBucketRequest, error) {
	if id == "" {
		return nil, fmt.Errorf("缺少存储桶 ID")
	}
	req := &serverv1.UpdateBucketRequest{Id: id, Public: public}
	if name != "" {
		req.Name = &name
	}
	return req, nil
}

// buildListFilesReq 构造 ListFilesRequest。
func buildListFilesReq(bucketID, queries string, pageSize int32, pageToken string) (*serverv1.ListFilesRequest, error) {
	if bucketID == "" {
		return nil, fmt.Errorf("缺少 bucket-id")
	}
	qs, err := jsonStringList(queries, "queries")
	if err != nil {
		return nil, err
	}
	req := &serverv1.ListFilesRequest{BucketId: bucketID, Queries: qs, PageToken: pageToken}
	if pageSize > 0 {
		req.PageSize = pageSize
	}
	return req, nil
}

// buildUpdateFileReq 构造 UpdateFileRequest（name/mime-type/metadata 至少一个）。
func buildUpdateFileReq(bucketID, fileID, name, mimeType, metadata string) (*serverv1.UpdateFileRequest, error) {
	if bucketID == "" {
		return nil, fmt.Errorf("缺少 bucket-id")
	}
	if fileID == "" {
		return nil, fmt.Errorf("缺少 file-id")
	}
	if name == "" && mimeType == "" && metadata == "" {
		return nil, fmt.Errorf("--name/--mime-type/--metadata 至少提供一个")
	}
	md, err := jsonStringMap(metadata, "metadata")
	if err != nil {
		return nil, err
	}
	req := &serverv1.UpdateFileRequest{BucketId: bucketID, FileId: fileID, Metadata: md}
	if name != "" {
		req.Name = &name
	}
	if mimeType != "" {
		req.MimeType = &mimeType
	}
	return req, nil
}
