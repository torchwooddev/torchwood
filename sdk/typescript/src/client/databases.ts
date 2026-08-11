import { listQuery, type HttpTransport } from "../http.js";
import type { Document, ListMeta, ListParams, UpdateDocumentInput, UpsertDocumentInput } from "../types.js";

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

  async listDocuments(
    databaseId: string,
    collectionId: string,
    params?: ListParams
  ): Promise<{ documents: Document[]; meta?: ListMeta }> {
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
    documentId: string
  ): Promise<void> {
    await this.http.request<void>(
      "DELETE",
      `/v1/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`
    );
  }

  async countDocuments(
    databaseId: string,
    collectionId: string,
    params?: Pick<ListParams, "queries">
  ): Promise<number> {
    const res = await this.http.request<{ count: number }>(
      "GET",
      `/v1/databases/${databaseId}/collections/${collectionId}/documents/count`,
      { query: listQuery(params) }
    );
    return res.count ?? 0;
  }
}
