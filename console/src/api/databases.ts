import { api } from "./client";

export interface Database {
  id: string;
  name: string;
  created_at: string;
  updated_at: string;
}

export interface Attribute {
  id: string;
  key: string;
  type: string;
  size?: number;
  required: boolean;
  array: boolean;
}

export interface Index {
  id: string;
  type: string;
  attributes: string[];
  orders: string[];
}

export interface Collection {
  id: string;
  database_id: string;
  name: string;
  permissions: string[];
  document_security?: boolean;
  disabled?: boolean;
  is_system: boolean;
  attributes: Attribute[];
  indexes: Index[];
  created_at: string;
  updated_at: string;
}

export interface Document {
  id: string;
  data: Record<string, unknown>;
  permissions?: string[];
  created_at: string;
  updated_at: string;
}

function normalizeIndex(index: Index): Index {
  return {
    ...index,
    attributes: index.attributes ?? [],
    orders: index.orders ?? [],
  };
}

function normalizeCollection(collection: Collection): Collection {
  return {
    ...collection,
    permissions: collection.permissions ?? [],
    attributes: collection.attributes ?? [],
    indexes: (collection.indexes ?? []).map(normalizeIndex),
    is_system: collection.is_system ?? false,
  };
}

export async function listDatabases(): Promise<Database[]> {
  const res = await api.get<{ databases: Database[] }>("/server/databases");
  return res.data.databases ?? [];
}

export async function getDatabase(id: string): Promise<Database> {
  const res = await api.get<Database>(`/server/databases/${id}`);
  return res.data;
}

export async function createDatabase(input: {
  id: string;
  name: string;
}): Promise<Database> {
  const res = await api.post<Database>("/server/databases", input);
  return res.data;
}

export async function deleteDatabase(id: string): Promise<void> {
  await api.delete(`/server/databases/${id}`);
}

export async function listCollections(databaseId: string): Promise<Collection[]> {
  const res = await api.get<{ collections: Collection[] }>(
    `/server/databases/${databaseId}/collections`
  );
  return (res.data.collections ?? []).map(normalizeCollection);
}

export async function getCollection(
  databaseId: string,
  collectionId: string
): Promise<Collection> {
  const res = await api.get<Collection>(
    `/server/databases/${databaseId}/collections/${collectionId}`
  );
  return normalizeCollection(res.data);
}

export async function createCollection(
  databaseId: string,
  input: { id: string; name: string; permissions?: string[]; document_security?: boolean }
): Promise<Collection> {
  const res = await api.post<Collection>(
    `/server/databases/${databaseId}/collections`,
    input
  );
  return normalizeCollection(res.data);
}

export async function deleteCollection(
  databaseId: string,
  collectionId: string
): Promise<void> {
  await api.delete(
    `/server/databases/${databaseId}/collections/${collectionId}`
  );
}

export async function updateCollection(
  databaseId: string,
  collectionId: string,
  input: {
    name?: string;
    permissions?: string[];
    document_security?: boolean;
    disabled?: boolean;
  }
): Promise<Collection> {
  const body: Record<string, unknown> = {};
  if (input.name !== undefined) body.name = input.name;
  if (input.document_security !== undefined) body.document_security = input.document_security;
  if (input.disabled !== undefined) body.disabled = input.disabled;
  if (input.permissions !== undefined) {
    body.permissions = { values: input.permissions };
  }
  const res = await api.patch<Collection>(
    `/server/databases/${databaseId}/collections/${collectionId}`,
    body
  );
  return normalizeCollection(res.data);
}

export async function createAttribute(
  databaseId: string,
  collectionId: string,
  input: {
    key: string;
    type: string;
    size?: number;
    required?: boolean;
    array?: boolean;
  }
): Promise<Attribute> {
  const res = await api.post<Attribute>(
    `/server/databases/${databaseId}/collections/${collectionId}/attributes`,
    input
  );
  return res.data;
}

export async function createIndex(
  databaseId: string,
  collectionId: string,
  input: {
    id: string;
    type: string;
    attributes: string[];
    orders?: string[];
  }
): Promise<Index> {
  const res = await api.post<Index>(
    `/server/databases/${databaseId}/collections/${collectionId}/indexes`,
    input
  );
  return res.data;
}

export async function deleteAttribute(
  databaseId: string,
  collectionId: string,
  key: string
): Promise<void> {
  await api.delete(
    `/server/databases/${databaseId}/collections/${collectionId}/attributes/${key}`
  );
}

export async function deleteIndex(
  databaseId: string,
  collectionId: string,
  indexId: string
): Promise<void> {
  await api.delete(
    `/server/databases/${databaseId}/collections/${collectionId}/indexes/${indexId}`
  );
}

export async function listDocuments(
  databaseId: string,
  collectionId: string
): Promise<Document[]> {
  const res = await api.get<{ documents: Document[] }>(
    `/server/databases/${databaseId}/collections/${collectionId}/documents`
  );
  return res.data.documents ?? [];
}

export async function getDocument(
  databaseId: string,
  collectionId: string,
  documentId: string
): Promise<Document> {
  const res = await api.get<Document>(
    `/server/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`
  );
  return res.data;
}

export async function createDocument(
  databaseId: string,
  collectionId: string,
  input: { data: Record<string, unknown>; document_id?: string; permissions?: string[] }
): Promise<Document> {
  const res = await api.post<Document>(
    `/server/databases/${databaseId}/collections/${collectionId}/documents`,
    input
  );
  return res.data;
}

export async function updateDocument(
  databaseId: string,
  collectionId: string,
  documentId: string,
  input: {
    data?: Record<string, unknown>;
    permissions?: string[];
    increment?: Record<string, number>;
  }
): Promise<Document> {
  const res = await api.patch<Document>(
    `/server/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`,
    input
  );
  return res.data;
}

export async function deleteDocument(
  databaseId: string,
  collectionId: string,
  documentId: string
): Promise<void> {
  await api.delete(
    `/server/databases/${databaseId}/collections/${collectionId}/documents/${documentId}`
  );
}

export async function bulkUpdateDocuments(
  databaseId: string,
  collectionId: string,
  input: {
    document_ids: string[];
    data: Record<string, unknown>;
    permissions?: string[];
  }
): Promise<number> {
  const res = await api.patch<{ affected: number }>(
    `/server/databases/${databaseId}/collections/${collectionId}/documents/bulk`,
    input
  );
  return res.data.affected ?? 0;
}

export async function bulkDeleteDocuments(
  databaseId: string,
  collectionId: string,
  documentIds: string[]
): Promise<number> {
  const res = await api.post<{ affected: number }>(
    `/server/databases/${databaseId}/collections/${collectionId}/documents/bulk/delete`,
    { document_ids: documentIds }
  );
  return res.data.affected ?? 0;
}
