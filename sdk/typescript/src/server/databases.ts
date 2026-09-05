import { listQuery, type HttpTransport } from "../http.js";
import type {
  Attribute,
  AttributeMigration,
  BulkDocumentsResponse,
  Collection,
  Database,
  Document,
  DocumentListParams,
  Index,
  ListChangesResponse,
  ListMeta,
  ListParams,
  UpdateDocumentInput,
  UpsertDocumentInput,
} from "../types.js";
import type { QueryAst } from "../query.js";

// documentsQueryBody 组装 POST :list 的 body（Query JSON；分页字段并入——
// page_size/page_token 与 query 内同名字段以 query 为准，不重复携带）。
function documentsQueryBody(
  query: QueryAst,
  pageSize?: number,
  pageToken?: string
): Record<string, unknown> {
  const body: Record<string, unknown> = { ...query };
  if (body.pageSize === undefined && pageSize !== undefined) {
    body.pageSize = pageSize;
  }
  if (body.pageToken === undefined && pageToken !== undefined) {
    body.pageToken = pageToken;
  }
  return body;
}

export class ServerDatabasesService {
  constructor(private readonly http: HttpTransport) {}

  async createDatabase(input: { id: string; name: string }): Promise<Database> {
    return this.http.request<Database>("POST", "/v1/server/databases", {
      auth: "apiKey",
      body: input,
    });
  }

  async listDatabases(
    params?: ListParams
  ): Promise<{ databases: Database[]; meta?: ListMeta }> {
    const res = await this.http.request<{ databases: Database[]; meta?: ListMeta }>(
      "GET",
      "/v1/server/databases",
      { auth: "apiKey", query: listQuery(params) }
    );
    return { databases: res.databases ?? [], meta: res.meta };
  }

  async getDatabase(id: string): Promise<Database> {
    return this.http.request<Database>("GET", `/v1/server/databases/${id}`, { auth: "apiKey" });
  }

  async deleteDatabase(id: string): Promise<void> {
    await this.http.request<void>("DELETE", `/v1/server/databases/${id}`, { auth: "apiKey" });
  }

  async createCollection(
    databaseId: string,
    input: {
      id: string;
      name: string;
      permissions?: string[];
      document_security?: boolean;
    }
  ): Promise<Collection> {
    return this.http.request<Collection>(
      "POST",
      `/v1/server/databases/${databaseId}/collections`,
      { auth: "apiKey", body: { database_id: databaseId, ...input } }
    );
  }

  async listCollections(
    databaseId: string,
    params?: ListParams
  ): Promise<{ collections: Collection[]; meta?: ListMeta }> {
    const res = await this.http.request<{ collections: Collection[]; meta?: ListMeta }>(
      "GET",
      `/v1/server/databases/${databaseId}/collections`,
      { auth: "apiKey", query: listQuery(params) }
    );
    return { collections: res.collections ?? [], meta: res.meta };
  }

  async getCollection(databaseId: string, collectionId: string): Promise<Collection> {
    return this.http.request<Collection>(
      "GET",
      `/v1/server/databases/${databaseId}/collections/${collectionId}`,
      { auth: "apiKey" }
    );
  }

  /**
   * 导出集合契约的 JSON Schema 2020-12 文档（B10，redesign §4.1/§10.1）。
   * 返回值即 schema 文档本体（$schema/$id/properties/required；系统字段
   * 以 readOnly 注释）。集合不存在抛 NotFound。
   */
  async exportCollectionSchema(databaseId: string, collectionId: string): Promise<Record<string, unknown>> {
    const res = await this.http.request<{ schema: Record<string, unknown> }>(
      "GET",
      `/v1/server/databases/${databaseId}/collections/${collectionId}:exportSchema`,
      { auth: "apiKey" }
    );
    return res.schema ?? {};
  }

  async updateCollection(
    databaseId: string,
    collectionId: string,
    input: {
      name?: string;
      permissions?: string[];
      document_security?: boolean;
      disabled?: boolean;
    }
  ): Promise<Collection> {
    const body: Record<string, unknown> = {
      database_id: databaseId,
      collection_id: collectionId,
    };
    if (input.name !== undefined) body.name = input.name;
    if (input.document_security !== undefined) body.document_security = input.document_security;
    if (input.disabled !== undefined) body.disabled = input.disabled;
    if (input.permissions !== undefined) {
      body.permissions = { values: input.permissions };
    }
    return this.http.request<Collection>(
      "PATCH",
      `/v1/server/databases/${databaseId}/collections/${collectionId}`,
      { auth: "apiKey", body }
    );
  }

  async deleteCollection(databaseId: string, collectionId: string): Promise<void> {
    await this.http.request<void>(
      "DELETE",
      `/v1/server/databases/${databaseId}/collections/${collectionId}`,
      { auth: "apiKey" }
    );
  }

  async createAttribute(
    databaseId: string,
    collectionId: string,
    input: {
      key: string;
      type: string;
      size?: number;
      required?: boolean;
      array?: boolean;
      default_value?: string;
    }
  ): Promise<Attribute> {
    return this.http.request<Attribute>(
      "POST",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/attributes`,
      {
        auth: "apiKey",
        body: { database_id: databaseId, collection_id: collectionId, ...input },
      }
    );
  }

  async deleteAttribute(
    databaseId: string,
    collectionId: string,
    key: string
  ): Promise<void> {
    await this.http.request<void>(
      "DELETE",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/attributes/${key}`,
      { auth: "apiKey" }
    );
  }

  /** 回滚属性生命周期（B4）：deprecated → active；migrating → 中止迁移。 */
  async restoreAttribute(
    databaseId: string,
    collectionId: string,
    key: string
  ): Promise<void> {
    await this.http.request<void>(
      "POST",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/attributes/${key}:restore`,
      { auth: "apiKey", body: {} }
    );
  }

  /** 物理删列（B4 删列两段段二，不可逆）。 */
  async retireAttribute(
    databaseId: string,
    collectionId: string,
    key: string
  ): Promise<void> {
    await this.http.request<void>(
      "POST",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/attributes/${key}:retire`,
      { auth: "apiKey", body: {} }
    );
  }

  /** 创建 copy 迁移任务（B4）：改类型/收紧异步回填 + 原子 swap；放宽即时。 */
  async migrateAttribute(
    databaseId: string,
    collectionId: string,
    key: string,
    input: {
      type: string;
      size?: number;
      required?: boolean;
      array?: boolean;
      default_value?: string;
    }
  ): Promise<AttributeMigration> {
    return this.http.request<AttributeMigration>(
      "POST",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/attributes/${key}:migrate`,
      {
        auth: "apiKey",
        body: { database_id: databaseId, collection_id: collectionId, ...input },
      }
    );
  }

  async createIndex(
    databaseId: string,
    collectionId: string,
    input: { id: string; type: string; attributes: string[]; orders?: string[] }
  ): Promise<Index> {
    return this.http.request<Index>(
      "POST",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/indexes`,
      {
        auth: "apiKey",
        body: { database_id: databaseId, collection_id: collectionId, ...input },
      }
    );
  }

  async deleteIndex(
    databaseId: string,
    collectionId: string,
    indexId: string
  ): Promise<void> {
    await this.http.request<void>(
      "DELETE",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/indexes/${indexId}`,
      { auth: "apiKey" }
    );
  }

  async createDocument(
    databaseId: string,
    collectionId: string,
    input: {
      document_id?: string;
      data: Record<string, unknown>;
      permissions?: string[];
    }
  ): Promise<Document> {
    return this.http.request<Document>(
      "POST",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents`,
      {
        auth: "apiKey",
        body: {
          database_id: databaseId,
          collection_id: collectionId,
          document_id: input.document_id ?? "",
          data: input.data,
          permissions: input.permissions,
        },
      }
    );
  }

  // C7 单 AST：带 query（过滤/排序/投影）时走 POST :list（body 即 Query
  // JSON，分页字段并入）；无 query 时保留 GET 简单分页（page_size/page_token）。
  // KNN（vector_search）查询时 distances 与 documents 平行回传（会话 #10）。
  async listDocuments(
    databaseId: string,
    collectionId: string,
    params?: DocumentListParams
  ): Promise<{ documents: Document[]; meta?: ListMeta; distances?: number[] }> {
    if (params?.query) {
      const body = documentsQueryBody(params.query, params.page_size, params.page_token);
      const res = await this.http.request<{
        documents: Document[];
        meta?: ListMeta;
        distances?: number[];
      }>("POST", `/v1/server/databases/${databaseId}/collections/${collectionId}/documents:list`, {
        auth: "apiKey",
        body,
      });
      return { documents: res.documents ?? [], meta: res.meta, distances: res.distances };
    }
    const res = await this.http.request<{ documents: Document[]; meta?: ListMeta }>(
      "GET",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents`,
      { auth: "apiKey", query: listQuery(params) }
    );
    return { documents: res.documents ?? [], meta: res.meta };
  }

  async getDocument(
    databaseId: string,
    collectionId: string,
    documentId: string
  ): Promise<Document> {
    return this.http.request<Document>(
      "GET",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`,
      { auth: "apiKey" }
    );
  }

  async updateDocument(
    databaseId: string,
    collectionId: string,
    documentId: string,
    input: UpdateDocumentInput
  ): Promise<Document> {
    return this.http.request<Document>(
      "PATCH",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`,
      {
        auth: "apiKey",
        body: {
          database_id: databaseId,
          collection_id: collectionId,
          document_id: documentId,
          ...input,
        },
      }
    );
  }

  async upsertDocument(
    databaseId: string,
    collectionId: string,
    documentId: string,
    input: UpsertDocumentInput
  ): Promise<Document> {
    return this.http.request<Document>(
      "PUT",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`,
      {
        auth: "apiKey",
        body: {
          database_id: databaseId,
          collection_id: collectionId,
          document_id: documentId,
          ...input,
        },
      }
    );
  }

  async deleteDocument(
    databaseId: string,
    collectionId: string,
    documentId: string,
    // 用户集合 OCC 必填：来自上一次 GetDocument/List 的 version。
    version: number
  ): Promise<void> {
    await this.http.request<void>(
      "DELETE",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`,
      { auth: "apiKey", query: { version } }
    );
  }

  // C7 单 AST：带 query 过滤时走 POST :count（GET 面仅支持无过滤计数）。
  async countDocuments(
    databaseId: string,
    collectionId: string,
    params?: Pick<DocumentListParams, "query">
  ): Promise<string | number> {
    if (params?.query) {
      const res = await this.http.request<{ count: string | number }>(
        "POST",
        `/v1/server/databases/${databaseId}/collections/${collectionId}/documents:count`,
        {
          auth: "apiKey",
          body: {
            database_id: databaseId,
            collection_id: collectionId,
            query: params.query,
          },
        }
      );
      return res.count ?? 0;
    }
    // int64：网关序列化为字符串（如 "42"）；消费时建议 Number() 归一化。
    // GET 面只做无过滤计数；有 query 时已在上方走 POST :count。
    const res = await this.http.request<{ count: string | number }>(
      "GET",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents:count`,
      { auth: "apiKey" }
    );
    return res.count ?? 0;
  }

  async bulkUpdateDocuments(
    databaseId: string,
    collectionId: string,
    input: {
      document_ids: string[];
      data?: Record<string, unknown>;
      permissions?: string[];
    }
  ): Promise<BulkDocumentsResponse> {
    return this.http.request<BulkDocumentsResponse>(
      "PATCH",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents:bulkUpdate`,
      {
        auth: "apiKey",
        body: { database_id: databaseId, collection_id: collectionId, ...input },
      }
    );
  }

  async bulkDeleteDocuments(
    databaseId: string,
    collectionId: string,
    documentIds: string[]
  ): Promise<BulkDocumentsResponse> {
    return this.http.request<BulkDocumentsResponse>(
      "POST",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents:bulkDelete`,
      {
        auth: "apiKey",
        body: {
          database_id: databaseId,
          collection_id: collectionId,
          document_ids: documentIds,
        },
      }
    );
  }

  // 拉取集合的已提交事件流（阶段④ §4.5 补偿 API，R17 补登）：seq 升序、
  // 按本 key 的可见性过滤。since_seq 为续传游标（0 = 从最老可用事件起）；
  // 游标早于重放窗口 → FailedPrecondition EVENTS.RESUME_EXPIRED（全量重拉后
  // 重新续传）。delete 事件无 data（tombstone：document_id + version 标识
  // 删除）；transaction_id 非空表示来自 execute-tx 原子批（批内顺序 = op 序）；
  // 按 event_id 幂等去重（at-leat-once）。续传**优先 next_since_seq**（R15
  // 两级语义），仅当为 0 时回退末条 change 的 seq；has_more=false 时恒为 0。
  async listChanges(
    databaseId: string,
    collectionId: string,
    params?: { since_seq?: number | string; limit?: number }
  ): Promise<ListChangesResponse> {
    const query: Record<string, string | number | undefined> = {};
    if (params?.since_seq !== undefined) query.since_seq = String(params.since_seq);
    if (params?.limit !== undefined) query.limit = params.limit;
    return this.http.request<ListChangesResponse>(
      "GET",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/changes`,
      { auth: "apiKey", query }
    );
  }

  // 聚合（sum/avg/min/max + 可选单键 group_by）。结果类型化（C7 预决策 5）：
  // integer 属性的 sum/min/max → int64_value（protojson 下为字符串）；
  // avg 与 float 属性 → double_value。query 为过滤 AST（排序/分页算子无意义）。
  async aggregateDocuments(
    databaseId: string,
    collectionId: string,
    input: {
      aggregations: { function: "SUM" | "AVG" | "MIN" | "MAX"; field: string }[];
      group_by?: string;
      query?: QueryAst;
    }
  ): Promise<AggregateDocumentsResponse> {
    return this.http.request<AggregateDocumentsResponse>(
      "POST",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents:aggregate`,
      {
        auth: "apiKey",
        body: {
          database_id: databaseId,
          collection_id: collectionId,
          aggregations: input.aggregations.map((a) => ({
            function: `AGGREGATE_FUNCTION_${a.function}`,
            field: a.field,
          })),
          group_by: input.group_by,
          query: input.query,
        },
      }
    );
  }

  // 事务内核 execute-tx（redesign §4.8 Phase 1）：单事务内异构 op 批。
  // mode 缺省/ATOMIC 任一失败整批回滚；PARTIAL 逐 op savepoint 容错并返回
  // per-op 结果。ops 的 expected_version 三态见 proto 注释。
  async executeTransactions(
    databaseId: string,
    ops: TransactionOp[],
    mode?: "ATOMIC" | "PARTIAL"
  ): Promise<{ results: TransactionOpResult[] }> {
    return this.http.request<{ results: TransactionOpResult[] }>(
      "POST",
      `/v1/server/databases/${databaseId}/documents:execute-tx`,
      {
        auth: "apiKey",
        body: {
          database_id: databaseId,
          ops,
          mode: mode ? `TRANSACTION_MODE_${mode}` : undefined,
        },
      }
    );
  }
}

/** 聚合项结果：oneof result（int64_value 网关序列化为字符串）。 */
export interface AggregateValue {
  function: string;
  field: string;
  int64_value?: string;
  double_value?: number;
}

export interface AggregateGroup {
  group_key?: string;
  values: AggregateValue[];
}

export interface AggregateDocumentsResponse {
  groups: AggregateGroup[];
}

/** execute-tx 的单个 op（字段族按 type 消费，见 proto TransactionOp 注释）。 */
export interface TransactionOp {
  type: "CREATE" | "UPDATE" | "UPSERT" | "DELETE";
  collection_id: string;
  document_id?: string;
  data?: Record<string, unknown>;
  permissions?: string[];
  increment?: Record<string, number>;
  expected_version?: string;
  conflict_columns?: string[];
}

export interface TransactionOpResult {
  index: number;
  status: "OK" | "ERROR";
  document_id?: string;
  version?: string;
  error_code?: string;
  error_message?: string;
}
