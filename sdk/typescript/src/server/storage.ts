import { listQuery, type HttpTransport } from "../http.js";
import type { Bucket, FileItem, FileToken, ListParams, StorageUsage } from "../types.js";

export class StorageService {
  constructor(private readonly http: HttpTransport) {}

  async createBucket(input: { name: string; permissions?: string[]; public?: boolean }): Promise<Bucket> {
    return this.http.request<Bucket>("POST", "/v1/server/storage/buckets", {
      auth: "apiKey",
      body: input,
    });
  }

  async listBuckets(params?: ListParams): Promise<Bucket[]> {
    const res = await this.http.request<{ buckets: Bucket[] }>(
      "GET",
      "/v1/server/storage/buckets",
      { auth: "apiKey", query: listQuery(params) }
    );
    return res.buckets ?? [];
  }

  async getBucket(id: string): Promise<Bucket> {
    return this.http.request<Bucket>("GET", `/v1/server/storage/buckets/${id}`, {
      auth: "apiKey",
    });
  }

  async deleteBucket(id: string): Promise<void> {
    await this.http.request<void>("DELETE", `/v1/server/storage/buckets/${id}`, {
      auth: "apiKey",
    });
  }

  async updateBucket(
    id: string,
    input: { name?: string; public?: boolean }
  ): Promise<Bucket> {
    return this.http.request<Bucket>("PATCH", `/v1/server/storage/buckets/${id}`, {
      auth: "apiKey",
      body: { id, ...input },
    });
  }

  async listFiles(bucketId: string, params?: ListParams): Promise<FileItem[]> {
    const res = await this.http.request<{ files: FileItem[] }>(
      "GET",
      `/v1/server/storage/buckets/${bucketId}/files`,
      { auth: "apiKey", query: listQuery(params) }
    );
    return res.files ?? [];
  }

  async getFile(bucketId: string, fileId: string): Promise<FileItem> {
    return this.http.request<FileItem>(
      "GET",
      `/v1/server/storage/buckets/${bucketId}/files/${fileId}`,
      { auth: "apiKey" }
    );
  }

  async deleteFile(bucketId: string, fileId: string): Promise<void> {
    await this.http.request<void>(
      "DELETE",
      `/v1/server/storage/buckets/${bucketId}/files/${fileId}`,
      { auth: "apiKey" }
    );
  }

  async updateFile(
    bucketId: string,
    fileId: string,
    input: { name?: string; mime_type?: string; metadata?: Record<string, string> }
  ): Promise<FileItem> {
    return this.http.request<FileItem>(
      "PATCH",
      `/v1/server/storage/buckets/${bucketId}/files/${fileId}`,
      {
        auth: "apiKey",
        body: { bucket_id: bucketId, file_id: fileId, ...input },
      }
    );
  }

  // 生成短期文件访问令牌（HMAC 签名）；token 仅本次返回，可拼接到
  // 下载 URL（?token=）匿名换取文件。
  async createFileToken(
    bucketId: string,
    fileId: string,
    input?: { expires_in?: number }
  ): Promise<FileToken> {
    return this.http.request<FileToken>(
      "POST",
      `/v1/server/storage/buckets/${bucketId}/files/${fileId}/tokens`,
      {
        auth: "apiKey",
        body: { bucket_id: bucketId, file_id: fileId, ...input },
      }
    );
  }

  async getStorageUsage(): Promise<StorageUsage> {
    return this.http.request<StorageUsage>("GET", "/v1/server/storage/usage", {
      auth: "apiKey",
    });
  }

  async uploadFile(
    bucketId: string,
    file: Blob,
    filename: string
  ): Promise<FileItem> {
    const form = new FormData();
    form.append("file", file, filename);
    return this.http.requestForm<FileItem>(
      "POST",
      `/v1/storage/buckets/${bucketId}/files`,
      form,
      "apiKey"
    );
  }
}
