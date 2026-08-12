import { listQuery, type HttpTransport } from "../http.js";
import type {
  Attribute,
  BulkDocumentsResponse,
  Collection,
  Database,
  Document,
  Index,
  ListMeta,
  ListParams,
  UpdateDocumentInput,
  UpsertDocumentInput,
} from "../types.js";

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

  async listDocuments(
    databaseId: string,
    collectionId: string,
    params?: ListParams
  ): Promise<{ documents: Document[]; meta?: ListMeta }> {
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
    documentId: string
  ): Promise<void> {
    await this.http.request<void>(
      "DELETE",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`,
      { auth: "apiKey" }
    );
  }

  async countDocuments(
    databaseId: string,
    collectionId: string,
    params?: Pick<ListParams, "queries">
  ): Promise<string | number> {
    // int64：网关序列化为字符串（如 "42"）；消费时建议 Number() 归一化。
    const res = await this.http.request<{ count: string | number }>(
      "GET",
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents/count`,
      { auth: "apiKey", query: listQuery(params) }
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
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents/bulk`,
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
      `/v1/server/databases/${databaseId}/collections/${collectionId}/documents/bulk/delete`,
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
}
