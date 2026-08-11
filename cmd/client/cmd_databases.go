package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
)

// newDatabasesCmd 覆盖 DatabasesService 全部 21 个方法：
// 库（create/list/get/delete）、集合（create/list/get/update/delete）、
// 属性（create/delete）、索引（create/delete）、文档（create/list/get/
// update/upsert/delete/count/bulk-update/bulk-delete）。
// 复杂结构（document data、queries、permissions 等）一律接受 JSON 字符串 flag。
func newDatabasesCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "databases",
		Short: "数据库管理（DatabasesService 全部方法：库/集合/属性/索引/文档）",
	}
	cmd.AddCommand(
		newDatabasesCreateCmd(g),
		newDatabasesListCmd(g),
		newDatabasesGetCmd(g),
		newDatabasesDeleteCmd(g),
		newDatabasesCollectionsCmd(g),
		newDatabasesAttributesCmd(g),
		newDatabasesIndexesCmd(g),
		newDatabasesDocumentsCmd(g),
	)
	return cmd
}

func newDatabasesCreateCmd(g *globalFlags) *cobra.Command {
	var id, name string
	cmd := &cobra.Command{
		Use:   "create --id <id> --name <name>",
		Short: "创建数据库",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateDatabaseReq(id, name)
			if err != nil {
				return err
			}
			resp := &serverv1.Database{}
			if err := invoke(g, serverv1.DatabasesService_CreateDatabase_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "数据库 ID（必填，小写字母/数字/下划线）")
	cmd.Flags().StringVar(&name, "name", "", "数据库名称（必填）")
	return cmd
}

func newDatabasesListCmd(g *globalFlags) *cobra.Command {
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出数据库",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.ListDatabasesResponse{}
			if err := invoke(g, serverv1.DatabasesService_ListDatabases_FullMethodName, buildListRequest(pageSize, pageToken), resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().Int32Var(&pageSize, "page-size", 0, "每页条数（服务端默认 50，上限 1000）")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "上一页返回的 next_page_token")
	return cmd
}

func newDatabasesGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "按 ID 获取数据库",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.Database{}
			if err := invoke(g, serverv1.DatabasesService_GetDatabase_FullMethodName, &serverv1.GetDatabaseRequest{Id: args[0]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newDatabasesDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "删除数据库（default 库不可删除）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.Database{}
			if err := invoke(g, serverv1.DatabasesService_DeleteDatabase_FullMethodName, &serverv1.GetDatabaseRequest{Id: args[0]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// newDatabasesCollectionsCmd: databases collections create/list/get/update/delete。
func newDatabasesCollectionsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collections",
		Short: "集合管理",
	}
	cmd.AddCommand(
		newDatabasesCollectionsCreateCmd(g),
		newDatabasesCollectionsListCmd(g),
		newDatabasesCollectionsGetCmd(g),
		newDatabasesCollectionsUpdateCmd(g),
		newDatabasesCollectionsDeleteCmd(g),
	)
	return cmd
}

func newDatabasesCollectionsCreateCmd(g *globalFlags) *cobra.Command {
	var id, name, permissions string
	var documentSecurity bool
	cmd := &cobra.Command{
		Use:   "create <database-id> --id <id> --name <name>",
		Short: "创建集合",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateCollectionReq(args[0], id, name, permissions, documentSecurity)
			if err != nil {
				return err
			}
			resp := &serverv1.Collection{}
			if err := invoke(g, serverv1.DatabasesService_CreateCollection_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "集合 ID（必填）")
	cmd.Flags().StringVar(&name, "name", "", "集合名称（必填）")
	cmd.Flags().StringVar(&permissions, "permissions", "", "权限 JSON 数组（如 '[\"read(\\\"users\\\")\"]'）")
	cmd.Flags().BoolVar(&documentSecurity, "document-security", false, "启用文档级安全")
	return cmd
}

func newDatabasesCollectionsListCmd(g *globalFlags) *cobra.Command {
	var queries string
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list <database-id>",
		Short: "列出集合",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildListCollectionsReq(args[0], queries, pageSize, pageToken)
			if err != nil {
				return err
			}
			resp := &serverv1.ListCollectionsResponse{}
			if err := invoke(g, serverv1.DatabasesService_ListCollections_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&queries, "queries", "", "Appwrite 风格查询 JSON 数组（如 '[\"equal(\\\"status\\\",\\\"active\\\")\"]'）")
	cmd.Flags().Int32Var(&pageSize, "page-size", 0, "每页条数（服务端默认 50，上限 1000）")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "上一页返回的 next_page_token")
	return cmd
}

func newDatabasesCollectionsGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <database-id> <collection-id>",
		Short: "按 ID 获取集合",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.Collection{}
			if err := invoke(g, serverv1.DatabasesService_GetCollection_FullMethodName, &serverv1.GetCollectionRequest{DatabaseId: args[0], CollectionId: args[1]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newDatabasesCollectionsUpdateCmd(g *globalFlags) *cobra.Command {
	var name, permissions string
	var documentSecurity, disabled bool
	cmd := &cobra.Command{
		Use:   "update <database-id> <collection-id> [--name] [--permissions] [--document-security] [--disabled]",
		Short: "更新集合（仅更新显式传入的字段）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateCollectionReq(args[0], args[1], name, permissions,
				changedBoolPtr(cmd, "document-security", documentSecurity),
				changedBoolPtr(cmd, "disabled", disabled))
			if err != nil {
				return err
			}
			resp := &serverv1.Collection{}
			if err := invoke(g, serverv1.DatabasesService_UpdateCollection_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "集合名称")
	cmd.Flags().StringVar(&permissions, "permissions", "", "权限 JSON 数组（全量替换）")
	cmd.Flags().BoolVar(&documentSecurity, "document-security", false, "文档级安全开关（显式传 --document-security=true/false 才生效）")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "禁用集合（显式传 --disabled=true/false 才生效）")
	return cmd
}

func newDatabasesCollectionsDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <database-id> <collection-id>",
		Short: "删除集合",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.Collection{}
			if err := invoke(g, serverv1.DatabasesService_DeleteCollection_FullMethodName, &serverv1.GetCollectionRequest{DatabaseId: args[0], CollectionId: args[1]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// newDatabasesAttributesCmd: databases attributes create/delete。
func newDatabasesAttributesCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attributes",
		Short: "集合属性管理",
	}
	cmd.AddCommand(
		newDatabasesAttributesCreateCmd(g),
		newDatabasesAttributesDeleteCmd(g),
	)
	return cmd
}

func newDatabasesAttributesCreateCmd(g *globalFlags) *cobra.Command {
	var key, typ, defaultValue string
	var size int32
	var required, array bool
	cmd := &cobra.Command{
		Use:   "create <database-id> <collection-id> --key <key> --type <type>",
		Short: "创建属性（类型：string/integer/float/boolean/datetime）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateAttributeReq(args[0], args[1], key, typ, size, required, array, defaultValue)
			if err != nil {
				return err
			}
			resp := &serverv1.Attribute{}
			if err := invoke(g, serverv1.DatabasesService_CreateAttribute_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "属性名（必填）")
	cmd.Flags().StringVar(&typ, "type", "", "属性类型（必填）")
	cmd.Flags().Int32Var(&size, "size", 0, "长度上限（string 属性）")
	cmd.Flags().BoolVar(&required, "required", false, "是否必填")
	cmd.Flags().BoolVar(&array, "array", false, "是否为数组")
	cmd.Flags().StringVar(&defaultValue, "default-value", "", "默认值（JSON 文本）")
	return cmd
}

func newDatabasesAttributesDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <database-id> <collection-id> <key>",
		Short: "删除属性",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.Attribute{}
			if err := invoke(g, serverv1.DatabasesService_DeleteAttribute_FullMethodName, &serverv1.DeleteAttributeRequest{DatabaseId: args[0], CollectionId: args[1], Key: args[2]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// newDatabasesIndexesCmd: databases indexes create/delete。
func newDatabasesIndexesCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "indexes",
		Short: "集合索引管理",
	}
	cmd.AddCommand(
		newDatabasesIndexesCreateCmd(g),
		newDatabasesIndexesDeleteCmd(g),
	)
	return cmd
}

func newDatabasesIndexesCreateCmd(g *globalFlags) *cobra.Command {
	var id, typ, attributes, orders string
	cmd := &cobra.Command{
		Use:   "create <database-id> <collection-id> --id <id> --type <type> --attributes '[...]'",
		Short: "创建索引（类型：key/unique/fulltext）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateIndexReq(args[0], args[1], id, typ, attributes, orders)
			if err != nil {
				return err
			}
			resp := &serverv1.Index{}
			if err := invoke(g, serverv1.DatabasesService_CreateIndex_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "索引 ID（必填）")
	cmd.Flags().StringVar(&typ, "type", "", "索引类型（必填：key/unique/fulltext）")
	cmd.Flags().StringVar(&attributes, "attributes", "", "索引属性 JSON 数组（必填）")
	cmd.Flags().StringVar(&orders, "orders", "", "排序 JSON 数组（如 '[\"asc\",\"desc\"]'）")
	return cmd
}

func newDatabasesIndexesDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <database-id> <collection-id> <index-id>",
		Short: "删除索引",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.Index{}
			if err := invoke(g, serverv1.DatabasesService_DeleteIndex_FullMethodName, &serverv1.DeleteIndexRequest{DatabaseId: args[0], CollectionId: args[1], IndexId: args[2]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

// newDatabasesDocumentsCmd: databases documents create/list/get/update/upsert/
// delete/count/bulk-update/bulk-delete。
func newDatabasesDocumentsCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "documents",
		Short: "文档管理",
	}
	cmd.AddCommand(
		newDatabasesDocumentsCreateCmd(g),
		newDatabasesDocumentsListCmd(g),
		newDatabasesDocumentsGetCmd(g),
		newDatabasesDocumentsUpdateCmd(g),
		newDatabasesDocumentsUpsertCmd(g),
		newDatabasesDocumentsDeleteCmd(g),
		newDatabasesDocumentsCountCmd(g),
		newDatabasesDocumentsBulkUpdateCmd(g),
		newDatabasesDocumentsBulkDeleteCmd(g),
	)
	return cmd
}

func newDatabasesDocumentsCreateCmd(g *globalFlags) *cobra.Command {
	var documentID, data, permissions string
	cmd := &cobra.Command{
		Use:   "create <database-id> <collection-id> --data '{...}'",
		Short: "创建文档（--data 为文档数据 JSON 对象）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildCreateDocumentReq(args[0], args[1], documentID, data, permissions)
			if err != nil {
				return err
			}
			resp := &serverv1.Document{}
			if err := invoke(g, serverv1.DatabasesService_CreateDocument_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&documentID, "document-id", "", "文档 ID（缺省自动生成）")
	cmd.Flags().StringVar(&data, "data", "", "文档数据 JSON 对象（必填，如 '{\"title\":\"hi\"}'）")
	cmd.Flags().StringVar(&permissions, "permissions", "", "权限 JSON 数组")
	return cmd
}

func newDatabasesDocumentsListCmd(g *globalFlags) *cobra.Command {
	var queries string
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use:   "list <database-id> <collection-id>",
		Short: "列出文档",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildListDocumentsReq(args[0], args[1], queries, pageSize, pageToken)
			if err != nil {
				return err
			}
			resp := &serverv1.ListDocumentsResponse{}
			if err := invoke(g, serverv1.DatabasesService_ListDocuments_FullMethodName, req, resp); err != nil {
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

func newDatabasesDocumentsGetCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <database-id> <collection-id> <document-id>",
		Short: "按 ID 获取文档",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.Document{}
			if err := invoke(g, serverv1.DatabasesService_GetDocument_FullMethodName, &serverv1.GetDocumentRequest{DatabaseId: args[0], CollectionId: args[1], DocumentId: args[2]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newDatabasesDocumentsUpdateCmd(g *globalFlags) *cobra.Command {
	var data, permissions, increment string
	cmd := &cobra.Command{
		Use:   "update <database-id> <collection-id> <document-id> [--data] [--permissions] [--increment]",
		Short: "更新文档（--data/--permissions/--increment 至少一个）",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpdateDocumentReq(args[0], args[1], args[2], data, permissions, increment)
			if err != nil {
				return err
			}
			resp := &serverv1.Document{}
			if err := invoke(g, serverv1.DatabasesService_UpdateDocument_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&data, "data", "", "文档数据 JSON 对象（全量替换）")
	cmd.Flags().StringVar(&permissions, "permissions", "", "权限 JSON 数组（全量替换）")
	cmd.Flags().StringVar(&increment, "increment", "", "自增字段 JSON 对象（如 '{\"views\":1}'）")
	return cmd
}

func newDatabasesDocumentsUpsertCmd(g *globalFlags) *cobra.Command {
	var data, permissions, conflictColumns string
	cmd := &cobra.Command{
		Use:   "upsert <database-id> <collection-id> <document-id> --data '{...}' --conflict-columns '[...]'",
		Short: "按 document-id 存在则更新、否则创建",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildUpsertDocumentReq(args[0], args[1], args[2], data, permissions, conflictColumns)
			if err != nil {
				return err
			}
			resp := &serverv1.Document{}
			if err := invoke(g, serverv1.DatabasesService_UpsertDocument_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&data, "data", "", "文档数据 JSON 对象（必填）")
	cmd.Flags().StringVar(&permissions, "permissions", "", "权限 JSON 数组")
	cmd.Flags().StringVar(&conflictColumns, "conflict-columns", "", "冲突列 JSON 数组（必填）")
	return cmd
}

func newDatabasesDocumentsDeleteCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <database-id> <collection-id> <document-id>",
		Short: "删除文档",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &serverv1.Document{}
			if err := invoke(g, serverv1.DatabasesService_DeleteDocument_FullMethodName, &serverv1.GetDocumentRequest{DatabaseId: args[0], CollectionId: args[1], DocumentId: args[2]}, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
}

func newDatabasesDocumentsCountCmd(g *globalFlags) *cobra.Command {
	var queries string
	cmd := &cobra.Command{
		Use:   "count <database-id> <collection-id>",
		Short: "统计匹配查询的文档数",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildListDocumentsReq(args[0], args[1], queries, 0, "")
			if err != nil {
				return err
			}
			resp := &serverv1.CountDocumentsResponse{}
			if err := invoke(g, serverv1.DatabasesService_CountDocuments_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&queries, "queries", "", "Appwrite 风格查询 JSON 数组")
	return cmd
}

func newDatabasesDocumentsBulkUpdateCmd(g *globalFlags) *cobra.Command {
	var documentIDs, data, permissions string
	cmd := &cobra.Command{
		Use:   "bulk-update <database-id> <collection-id> --document-ids '[...]' --data '{...}'",
		Short: "批量更新文档（共享同一份 data/permissions）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildBulkUpdateDocumentsReq(args[0], args[1], documentIDs, data, permissions)
			if err != nil {
				return err
			}
			resp := &serverv1.BulkDocumentsResponse{}
			if err := invoke(g, serverv1.DatabasesService_BulkUpdateDocuments_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&documentIDs, "document-ids", "", "文档 ID JSON 数组（必填）")
	cmd.Flags().StringVar(&data, "data", "", "共享更新数据 JSON 对象（必填）")
	cmd.Flags().StringVar(&permissions, "permissions", "", "权限 JSON 数组")
	return cmd
}

func newDatabasesDocumentsBulkDeleteCmd(g *globalFlags) *cobra.Command {
	var documentIDs string
	cmd := &cobra.Command{
		Use:   "bulk-delete <database-id> <collection-id> --document-ids '[...]'",
		Short: "批量删除文档",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildBulkDeleteDocumentsReq(args[0], args[1], documentIDs)
			if err != nil {
				return err
			}
			resp := &serverv1.BulkDocumentsResponse{}
			if err := invoke(g, serverv1.DatabasesService_BulkDeleteDocuments_FullMethodName, req, resp); err != nil {
				return err
			}
			return printJSON(os.Stdout, resp)
		},
	}
	cmd.Flags().StringVar(&documentIDs, "document-ids", "", "文档 ID JSON 数组（必填）")
	return cmd
}

// buildCreateDatabaseReq 构造 CreateDatabaseRequest（id/name 必填，与
// 服务端校验一致）。
func buildCreateDatabaseReq(id, name string) (*serverv1.CreateDatabaseRequest, error) {
	if id == "" {
		return nil, fmt.Errorf("--id 必填")
	}
	if name == "" {
		return nil, fmt.Errorf("--name 必填")
	}
	return &serverv1.CreateDatabaseRequest{Id: id, Name: name}, nil
}

// buildCreateCollectionReq 构造 CreateCollectionRequest。
func buildCreateCollectionReq(databaseID, id, name, permissions string, documentSecurity bool) (*serverv1.CreateCollectionRequest, error) {
	if databaseID == "" {
		return nil, fmt.Errorf("缺少 database-id")
	}
	if id == "" {
		return nil, fmt.Errorf("--id 必填")
	}
	if name == "" {
		return nil, fmt.Errorf("--name 必填")
	}
	perms, err := jsonStringList(permissions, "permissions")
	if err != nil {
		return nil, err
	}
	return &serverv1.CreateCollectionRequest{
		DatabaseId:       databaseID,
		Id:               id,
		Name:             name,
		Permissions:      perms,
		DocumentSecurity: &documentSecurity,
	}, nil
}

// buildListCollectionsReq 构造 ListCollectionsRequest（queries 数组 + 分页）。
func buildListCollectionsReq(databaseID, queries string, pageSize int32, pageToken string) (*serverv1.ListCollectionsRequest, error) {
	if databaseID == "" {
		return nil, fmt.Errorf("缺少 database-id")
	}
	qs, err := jsonStringList(queries, "queries")
	if err != nil {
		return nil, err
	}
	req := &serverv1.ListCollectionsRequest{DatabaseId: databaseID, Queries: qs, PageToken: pageToken}
	if pageSize > 0 {
		req.PageSize = pageSize
	}
	return req, nil
}

// buildUpdateCollectionReq 构造 UpdateCollectionRequest：仅设置显式传入的字段。
func buildUpdateCollectionReq(databaseID, collectionID, name, permissions string, documentSecurity, disabled *bool) (*serverv1.UpdateCollectionRequest, error) {
	if databaseID == "" {
		return nil, fmt.Errorf("缺少 database-id")
	}
	if collectionID == "" {
		return nil, fmt.Errorf("缺少 collection-id")
	}
	req := &serverv1.UpdateCollectionRequest{
		DatabaseId:       databaseID,
		CollectionId:     collectionID,
		DocumentSecurity: documentSecurity,
		Disabled:         disabled,
	}
	if name != "" {
		req.Name = name
	}
	if permissions != "" {
		values, err := jsonStringList(permissions, "permissions")
		if err != nil {
			return nil, err
		}
		req.Permissions = &serverv1.PermissionsUpdate{Values: values}
	}
	return req, nil
}

// buildCreateAttributeReq 构造 CreateAttributeRequest（key/type 必填）。
func buildCreateAttributeReq(databaseID, collectionID, key, typ string, size int32, required, array bool, defaultValue string) (*serverv1.CreateAttributeRequest, error) {
	if databaseID == "" {
		return nil, fmt.Errorf("缺少 database-id")
	}
	if collectionID == "" {
		return nil, fmt.Errorf("缺少 collection-id")
	}
	if key == "" {
		return nil, fmt.Errorf("--key 必填")
	}
	if typ == "" {
		return nil, fmt.Errorf("--type 必填")
	}
	return &serverv1.CreateAttributeRequest{
		DatabaseId:   databaseID,
		CollectionId: collectionID,
		Key:          key,
		Type:         typ,
		Size:         size,
		Required:     required,
		Array:        array,
		DefaultValue: defaultValue,
	}, nil
}

// buildCreateIndexReq 构造 CreateIndexRequest（id/type/attributes 必填）。
func buildCreateIndexReq(databaseID, collectionID, id, typ, attributes, orders string) (*serverv1.CreateIndexRequest, error) {
	if databaseID == "" {
		return nil, fmt.Errorf("缺少 database-id")
	}
	if collectionID == "" {
		return nil, fmt.Errorf("缺少 collection-id")
	}
	if id == "" {
		return nil, fmt.Errorf("--id 必填")
	}
	if typ == "" {
		return nil, fmt.Errorf("--type 必填")
	}
	attrs, err := jsonStringList(attributes, "attributes")
	if err != nil {
		return nil, err
	}
	if len(attrs) == 0 {
		return nil, fmt.Errorf("--attributes 必填")
	}
	ords, err := jsonStringList(orders, "orders")
	if err != nil {
		return nil, err
	}
	return &serverv1.CreateIndexRequest{
		DatabaseId:   databaseID,
		CollectionId: collectionID,
		Id:           id,
		Type:         typ,
		Attributes:   attrs,
		Orders:       ords,
	}, nil
}

// buildCreateDocumentReq 构造 CreateDocumentRequest（--data 为文档数据本体）。
func buildCreateDocumentReq(databaseID, collectionID, documentID, data, permissions string) (*serverv1.CreateDocumentRequest, error) {
	if databaseID == "" {
		return nil, fmt.Errorf("缺少 database-id")
	}
	if collectionID == "" {
		return nil, fmt.Errorf("缺少 collection-id")
	}
	docData, err := structData(data)
	if err != nil {
		return nil, err
	}
	if docData == nil {
		return nil, fmt.Errorf("--data 必填（文档数据 JSON 对象）")
	}
	perms, err := jsonStringList(permissions, "permissions")
	if err != nil {
		return nil, err
	}
	return &serverv1.CreateDocumentRequest{
		DatabaseId:   databaseID,
		CollectionId: collectionID,
		DocumentId:   documentID,
		Data:         docData,
		Permissions:  perms,
	}, nil
}

// buildListDocumentsReq 构造 ListDocumentsRequest（供 list/count 复用）。
func buildListDocumentsReq(databaseID, collectionID, queries string, pageSize int32, pageToken string) (*serverv1.ListDocumentsRequest, error) {
	if databaseID == "" {
		return nil, fmt.Errorf("缺少 database-id")
	}
	if collectionID == "" {
		return nil, fmt.Errorf("缺少 collection-id")
	}
	qs, err := jsonStringList(queries, "queries")
	if err != nil {
		return nil, err
	}
	req := &serverv1.ListDocumentsRequest{DatabaseId: databaseID, CollectionId: collectionID, Queries: qs, PageToken: pageToken}
	if pageSize > 0 {
		req.PageSize = pageSize
	}
	return req, nil
}

// buildUpdateDocumentReq 构造 UpdateDocumentRequest（data/permissions/increment
// 至少一个，与服务端校验一致）。
func buildUpdateDocumentReq(databaseID, collectionID, documentID, data, permissions, increment string) (*serverv1.UpdateDocumentRequest, error) {
	if databaseID == "" {
		return nil, fmt.Errorf("缺少 database-id")
	}
	if collectionID == "" {
		return nil, fmt.Errorf("缺少 collection-id")
	}
	if documentID == "" {
		return nil, fmt.Errorf("缺少 document-id")
	}
	if data == "" && permissions == "" && increment == "" {
		return nil, fmt.Errorf("--data/--permissions/--increment 至少提供一个")
	}
	docData, err := structData(data)
	if err != nil {
		return nil, err
	}
	perms, err := jsonStringList(permissions, "permissions")
	if err != nil {
		return nil, err
	}
	incr, err := jsonInt64Map(increment, "increment")
	if err != nil {
		return nil, err
	}
	return &serverv1.UpdateDocumentRequest{
		DatabaseId:   databaseID,
		CollectionId: collectionID,
		DocumentId:   documentID,
		Data:         docData,
		Permissions:  perms,
		Increment:    incr,
	}, nil
}

// buildUpsertDocumentReq 构造 UpsertDocumentRequest（data/conflict-columns 必填）。
func buildUpsertDocumentReq(databaseID, collectionID, documentID, data, permissions, conflictColumns string) (*serverv1.UpsertDocumentRequest, error) {
	if databaseID == "" {
		return nil, fmt.Errorf("缺少 database-id")
	}
	if collectionID == "" {
		return nil, fmt.Errorf("缺少 collection-id")
	}
	if documentID == "" {
		return nil, fmt.Errorf("缺少 document-id")
	}
	docData, err := structData(data)
	if err != nil {
		return nil, err
	}
	if docData == nil {
		return nil, fmt.Errorf("--data 必填（文档数据 JSON 对象）")
	}
	cols, err := jsonStringList(conflictColumns, "conflict-columns")
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("--conflict-columns 必填")
	}
	perms, err := jsonStringList(permissions, "permissions")
	if err != nil {
		return nil, err
	}
	return &serverv1.UpsertDocumentRequest{
		DatabaseId:      databaseID,
		CollectionId:    collectionID,
		DocumentId:      documentID,
		Data:            docData,
		Permissions:     perms,
		ConflictColumns: cols,
	}, nil
}

// buildBulkUpdateDocumentsReq 构造 BulkUpdateDocumentsRequest。
func buildBulkUpdateDocumentsReq(databaseID, collectionID, documentIDs, data, permissions string) (*serverv1.BulkUpdateDocumentsRequest, error) {
	if databaseID == "" {
		return nil, fmt.Errorf("缺少 database-id")
	}
	if collectionID == "" {
		return nil, fmt.Errorf("缺少 collection-id")
	}
	ids, err := jsonStringList(documentIDs, "document-ids")
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("--document-ids 必填")
	}
	docData, err := structData(data)
	if err != nil {
		return nil, err
	}
	if docData == nil {
		return nil, fmt.Errorf("--data 必填（共享更新数据 JSON 对象）")
	}
	perms, err := jsonStringList(permissions, "permissions")
	if err != nil {
		return nil, err
	}
	return &serverv1.BulkUpdateDocumentsRequest{
		DatabaseId:   databaseID,
		CollectionId: collectionID,
		DocumentIds:  ids,
		Data:         docData,
		Permissions:  perms,
	}, nil
}

// buildBulkDeleteDocumentsReq 构造 BulkDeleteDocumentsRequest。
func buildBulkDeleteDocumentsReq(databaseID, collectionID, documentIDs string) (*serverv1.BulkDeleteDocumentsRequest, error) {
	if databaseID == "" {
		return nil, fmt.Errorf("缺少 database-id")
	}
	if collectionID == "" {
		return nil, fmt.Errorf("缺少 collection-id")
	}
	ids, err := jsonStringList(documentIDs, "document-ids")
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("--document-ids 必填")
	}
	return &serverv1.BulkDeleteDocumentsRequest{
		DatabaseId:   databaseID,
		CollectionId: collectionID,
		DocumentIds:  ids,
	}, nil
}
