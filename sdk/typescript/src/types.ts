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
  // protojson 将 int64 序列化为字符串（如 "1800000"）；兼容旧 number 响应。
  expires_at: string | number;
}

// SignUp/SignIn 等认证响应；mfa_required 时无 tokens，应先返回
// challenge_token 引导二次认证（调用方不得在 mfa_required 分支访问 tokens）。
export interface AuthResult {
  account: Account;
  tokens: TokenBundle;
  mfa_required?: boolean;
  challenge_token?: string;
  factors?: Factor[];
}

export interface Factor {
  id: string;
  type: string;
  status: string;
  created_at?: string;
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
  // proto 为 Struct：直接透传对象（勿用 {values:[...]} 包装）。
  labels?: Record<string, unknown>;
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
  // int64：网关序列化为字符串。
  affected: string | number;
}

export interface UpdateDocumentInput {
  data?: Record<string, unknown>;
  permissions?: string[];
  increment?: Record<string, number>;
}

export interface UpsertDocumentInput {
  data: Record<string, unknown>;
  permissions?: string[];
  conflict_columns: string[];
}

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
  // int64：网关序列化为字符串。
  size: string | number;
  metadata?: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface FileToken {
  // 仅创建响应返回一次，之后任何接口不回显。
  token: string;
  expires_at: string;
}

export interface StorageUsage {
  buckets: number;
  files: number;
  // int64：网关序列化为字符串。
  total_size: string | number;
}

export interface FunctionInfo {
  id: string;
  project_id: string;
  name: string;
  runtime: string;
  entrypoint: string;
  timeout_seconds: number;
  spec: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface RuntimeInfo {
  id: string;
  name: string;
  entrypoint: string;
}

export interface SpecificationInfo {
  id: string;
  cpu: string;
  memory: string;
}

export interface Deployment {
  id: string;
  function_id: string;
  // int64：网关序列化为字符串。
  size: string | number;
  status: string;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface Variable {
  key: string;
  // 敏感数据：调用方不得持久化/日志输出明文。
  value: string;
}

export interface Execution {
  id: string;
  function_id: string;
  deployment_id: string;
  status: string;
  response?: string;
  stdout?: string;
  stderr?: string;
  status_code?: number;
  // int64：网关序列化为字符串。
  duration_ms: string | number;
  error?: string;
  response_truncated?: boolean;
  stdout_truncated?: boolean;
  stderr_truncated?: boolean;
  created_at: string;
  updated_at: string;
}

export interface LogEntry {
  id: string;
  action: string;
  status: string;
  resource_id?: string;
  ip: string;
  user_agent: string;
  created_at: string;
}

export interface TOTPFactor {
  factor: Factor;
  // 仅创建响应返回明文 secret（otpauth_url 亦含 secret），之后不回显。
  secret?: string;
  otpauth_url?: string;
}
