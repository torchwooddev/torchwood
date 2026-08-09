export interface ListMeta {
  page_size?: number;
  total_count?: number;
  next_page_token?: string;
}

export interface ListParams {
  queries?: string[];
  page_size?: number;
  page_token?: string;
}

export interface Account {
  id: string;
  email: string;
  name: string;
  status: string;
  email_verified: boolean;
  created_at: string;
  updated_at: string;
}

export interface TokenBundle {
  access_token: string;
  refresh_token: string;
  expires_at: number;
}

export interface Session {
  id: string;
  user_id: string;
  provider: string;
  user_agent: string;
  ip: string;
  expire_at: string;
  created_at: string;
  current: boolean;
}

export interface Document {
  id: string;
  data: Record<string, unknown>;
  permissions?: string[];
  created_at: string;
  updated_at: string;
}

export interface Team {
  id: string;
  name: string;
  total: number;
  permissions?: string[];
  created_at: string;
  updated_at: string;
}

export interface Membership {
  id: string;
  team_id: string;
  user_id: string;
  email: string;
  name: string;
  roles: string[];
  status: string;
  invited_at?: string;
  joined_at?: string;
  created_at: string;
  updated_at: string;
}

export interface Project {
  id: string;
  name: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface APIKey {
  id: string;
  name: string;
  scopes: string[];
  enabled: boolean;
  expire_at?: string;
  created_at: string;
  updated_at: string;
}

export interface User {
  id: string;
  email: string;
  name: string;
  status: string;
  email_verified: boolean;
  labels?: string[];
  prefs?: Record<string, unknown>;
  phone?: string;
  created_at: string;
  updated_at: string;
}

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
  default_value?: string;
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
  attributes: Attribute[];
  indexes: Index[];
  document_security?: boolean;
  disabled?: boolean;
  is_system?: boolean;
  created_at: string;
  updated_at: string;
}

export interface BulkDocumentsResponse {
  affected: number;
}

export interface UpdateDocumentInput {
  data?: Record<string, unknown>;
  permissions?: string[];
  increment?: Record<string, number>;
}

export interface Bucket {
  id: string;
  name: string;
  permissions: string[];
  created_at: string;
  updated_at: string;
}

export interface FileItem {
  id: string;
  bucket_id: string;
  name: string;
  mime_type: string;
  size: number;
  created_at: string;
  updated_at: string;
}
