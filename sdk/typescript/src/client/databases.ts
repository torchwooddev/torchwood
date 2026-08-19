import { listQuery, type HttpTransport } from "../http.js";
import type {
  CreateTransactionDocumentInput,
  Document,
  ListMeta,
  ListParams,
  Transaction,
  TransactionOp,
  UpdateDocumentInput,
  UpdateTransactionDocumentInput,
  UpsertDocumentInput,
  UpsertTransactionDocumentInput,
} from "../types.js";

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

  async countDocuments(
    databaseId: string,
    collectionId: string,
    params?: Pick<ListParams, "queries">
  ): Promise<string | number> {
    // int64：网关序列化为字符串（如 "42"）；消费时建议 Number() 归一化。
    const res = await this.http.request<{ count: string | number }>(
      "GET",
      `/v1/databases/${databaseId}/collections/${collectionId}/documents:count`,
      { query: listQuery(params) }
    );
    return res.count ?? 0;
  }

  // ---- 单库事务（v2 设计 §5）----

  async createTransaction(databaseId: string): Promise<Transaction> {
    return this.http.request<Transaction>(
      "POST",
      `/v1/databases/${databaseId}/transactions`,
      { body: { database_id: databaseId } }
    );
  }

  async getTransaction(databaseId: string, transactionId: string): Promise<Transaction> {
    return this.http.request<Transaction>(
      "GET",
      `/v1/databases/${databaseId}/transactions/${transactionId}`
    );
  }

  async createTransactionDocument(
    databaseId: string,
    transactionId: string,
    input: CreateTransactionDocumentInput
  ): Promise<TransactionOp> {
    return this.http.request<TransactionOp>(
      "POST",
      `/v1/databases/${databaseId}/transactions/${transactionId}/documents`,
      {
        body: {
          database_id: databaseId,
          transaction_id: transactionId,
          collection_id: input.collection_id,
          document_id: input.document_id ?? "",
          data: input.data,
          permissions: input.permissions,
        },
      }
    );
  }

  async updateTransactionDocument(
    databaseId: string,
    transactionId: string,
    collectionId: string,
    documentId: string,
    input: UpdateTransactionDocumentInput
  ): Promise<TransactionOp> {
    return this.http.request<TransactionOp>(
      "PATCH",
      `/v1/databases/${databaseId}/transactions/${transactionId}/collections/${collectionId}/documents/${documentId}`,
      {
        body: {
          database_id: databaseId,
          transaction_id: transactionId,
          collection_id: collectionId,
          document_id: documentId,
          ...input,
        },
      }
    );
  }

  async deleteTransactionDocument(
    databaseId: string,
    transactionId: string,
    collectionId: string,
    documentId: string,
    // 用户集合 OCC 必填。
    version: number
  ): Promise<TransactionOp> {
    return this.http.request<TransactionOp>(
      "DELETE",
      `/v1/databases/${databaseId}/transactions/${transactionId}/collections/${collectionId}/documents/${documentId}`,
      { query: { version } }
    );
  }

  async upsertTransactionDocument(
    databaseId: string,
    transactionId: string,
    collectionId: string,
    documentId: string,
    input: UpsertTransactionDocumentInput
  ): Promise<TransactionOp> {
    return this.http.request<TransactionOp>(
      "PUT",
      `/v1/databases/${databaseId}/transactions/${transactionId}/collections/${collectionId}/documents/${documentId}`,
      {
        body: {
          database_id: databaseId,
          transaction_id: transactionId,
          collection_id: collectionId,
          document_id: documentId,
          ...input,
        },
      }
    );
  }

  // 单段事务应用全部操作并写 outbox；任一操作失败整单回滚。
  async commitTransaction(databaseId: string, transactionId: string): Promise<Transaction> {
    return this.http.request<Transaction>(
      "POST",
      `/v1/databases/${databaseId}/transactions/${transactionId}:commit`,
      { body: { database_id: databaseId, transaction_id: transactionId } }
    );
  }

  async rollbackTransaction(databaseId: string, transactionId: string): Promise<Transaction> {
    return this.http.request<Transaction>(
      "DELETE",
      `/v1/databases/${databaseId}/transactions/${transactionId}`
    );
  }
}
