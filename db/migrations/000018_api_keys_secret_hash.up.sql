-- 认证热路径按 secret_hash 点查；今日无索引。唯一约束即 hash 全局唯一不变式。
CREATE UNIQUE INDEX IF NOT EXISTS api_keys_secret_hash_key ON api_keys (secret_hash);
