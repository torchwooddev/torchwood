import { listQuery, type HttpTransport } from "../http.js";
import type {
  Change,
  Document,
  DocumentListParams,
  ListChangesResponse,
  ListMeta,
  UpdateDocumentInput,
  UpsertDocumentInput,
} from "../types.js";
import type { QueryAst } from "../query.js";

export class ClientDatabasesService {
  constructor(private readonly http: HttpTransport) {}

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
      `/v1/databases/${databaseId}/collections/${collectionId}/documents`,
      {
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
  async listDocuments(
    databaseId: string,
    collectionId: string,
    params?: DocumentListParams
  ): Promise<{ documents: Document[]; meta?: ListMeta }> {
    if (params?.query) {
      const body = documentsQueryBody(params.query, params.page_size, params.page_token);
      const res = await this.http.request<{ documents: Document[]; meta?: ListMeta }>(
        "POST",
        `/v1/databases/${databaseId}/collections/${collectionId}/documents:list`,
        { body }
      );
      return { documents: res.documents ?? [], meta: res.meta };
    }
    const res = await this.http.request<{
      documents: Document[];
      meta?: ListMeta;
    }>("GET", `/v1/databases/${databaseId}/collections/${collectionId}/documents`, {
      query: listQuery(params),
    });
    return { documents: res.documents ?? [], meta: res.meta };
  }

  async getDocument(
    databaseId: string,
    collectionId: string,
    documentId: string
  ): Promise<Document> {
    return this.http.request<Document>(
      "GET",
      `/v1/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`
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
      `/v1/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`,
      {
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
      `/v1/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`,
      {
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
      `/v1/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`,
      { query: { version } }
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
        `/v1/databases/${databaseId}/collections/${collectionId}/documents:count`,
        {
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
      `/v1/databases/${databaseId}/collections/${collectionId}/documents:count`
    );
    return res.count ?? 0;
  }

  // 拉取集合的已提交事件流（阶段④ §4.5 补偿 API，R17 补登）：seq 升序、
  // 按当前用户可见性过滤。since_seq 为续传游标（0 = 从最老可用事件起）；
  // 游标早于重放窗口 → FailedPrecondition EVENTS.RESUME_EXPIRED（全量重拉后
  // 重新续传）。delete 事件无 data（tombstone）；按 event_id 幂等去重
  //（at-least-once）。续传**优先 next_since_seq**（R15 两级语义），仅当为 0
  // 时回退末条 change 的 seq。
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
      `/v1/databases/${databaseId}/collections/${collectionId}/changes`,
      { query }
    );
  }
}

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
