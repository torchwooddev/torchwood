import { api } from "./client";

export interface Bucket {
  id: string;
  name: string;
  permissions: string[];
  public?: boolean;
  created_at: string;
  updated_at: string;
}

export interface FileItem {
  id: string;
  bucket_id: string;
  name: string;
  mime_type: string;
  size: number;
  metadata?: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface StorageUsage {
  buckets: number;
  files: number;
  total_size: number;
}

function normalizeBucket(bucket: Bucket): Bucket {
  return {
    ...bucket,
    public: bucket.public ?? false,
    permissions: bucket.permissions ?? [],
  };
}

export async function listBuckets(): Promise<Bucket[]> {
  const res = await api.get<{ buckets: Bucket[] }>("/server/storage/buckets");
  return (res.data.buckets ?? []).map(normalizeBucket);
}

export async function getBucket(id: string): Promise<Bucket> {
  const res = await api.get<Bucket>(`/server/storage/buckets/${id}`);
  return normalizeBucket(res.data);
}

export async function createBucket(input: {
  name: string;
  public?: boolean;
}): Promise<Bucket> {
  const res = await api.post<Bucket>("/server/storage/buckets", input);
  return normalizeBucket(res.data);
}

export async function deleteBucket(id: string): Promise<void> {
  await api.delete(`/server/storage/buckets/${id}`);
}

export async function updateBucket(
  id: string,
  input: { name?: string; public?: boolean }
): Promise<Bucket> {
  const res = await api.patch<Bucket>(`/server/storage/buckets/${id}`, input);
  return normalizeBucket(res.data);
}

export async function listFiles(bucketId: string): Promise<FileItem[]> {
  const res = await api.get<{ files: FileItem[] }>(
    `/server/storage/buckets/${bucketId}/files`
  );
  return res.data.files ?? [];
}

export async function getFile(bucketId: string, fileId: string): Promise<FileItem> {
  const res = await api.get<FileItem>(
    `/server/storage/buckets/${bucketId}/files/${fileId}`
  );
  return res.data;
}

export async function uploadFile(
  bucketId: string,
  file: File
): Promise<FileItem> {
  const form = new FormData();
  form.append("file", file);
  const res = await api.post<FileItem>(
    `/storage/buckets/${bucketId}/files`,
    form,
    {
      headers: { "Content-Type": "multipart/form-data" },
    }
  );
  return res.data;
}

function filenameFromDisposition(header: string | undefined): string | undefined {
  if (!header) return undefined;
  const encoded = header.match(/filename\*=UTF-8''([^;\n]+)/i);
  if (encoded?.[1]) {
    try {
      return decodeURIComponent(encoded[1]);
    } catch {
      // fall through to ASCII filename
    }
  }
  const ascii = header.match(/filename="([^"]+)"/);
  return ascii?.[1];
}

export async function downloadFile(
  bucketId: string,
  fileId: string,
  fallbackName?: string
): Promise<void> {
  const res = await api.get(
    `/storage/buckets/${bucketId}/files/${fileId}/download`,
    { responseType: "blob" }
  );
  const blob = res.data as Blob;
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download =
    fallbackName ??
    filenameFromDisposition(res.headers["content-disposition"]) ??
    "download";
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

export async function deleteFile(bucketId: string, fileId: string): Promise<void> {
  await api.delete(`/server/storage/buckets/${bucketId}/files/${fileId}`);
}

export async function updateFile(
  bucketId: string,
  fileId: string,
  input: {
    name?: string;
    mime_type?: string;
    metadata?: Record<string, string>;
  }
): Promise<FileItem> {
  const res = await api.patch<FileItem>(
    `/server/storage/buckets/${bucketId}/files/${fileId}`,
    input
  );
  return res.data;
}

export async function createFileToken(
  bucketId: string,
  fileId: string,
  expiresIn?: number
): Promise<{ token: string; expires_at: string }> {
  const res = await api.post<{ token: string; expires_at: string }>(
    `/server/storage/buckets/${bucketId}/files/${fileId}/tokens`,
    { expires_in: expiresIn }
  );
  return res.data;
}

export async function getStorageUsage(): Promise<StorageUsage> {
  const res = await api.get<StorageUsage>("/server/storage/usage");
  return res.data;
}

// fileViewUrl 构造内联查看 URL（浏览器可直接打开）。
export function fileViewUrl(bucketId: string, fileId: string): string {
  return `/v1/storage/buckets/${bucketId}/files/${fileId}/view`;
}

// filePreviewUrl 构造缩略图 URL（带鉴权 cookie，浏览器 img 标签可加载）。
export function filePreviewUrl(bucketId: string, fileId: string): string {
  return `/v1/storage/buckets/${bucketId}/files/${fileId}/preview?width=200`;
}
