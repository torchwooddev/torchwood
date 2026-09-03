-- 写幂等缓存（redesign §4.1/§10.1）：request_id 24h 去重，只缓存成功响应。
-- 键作用域 (project_id, actor_id, request_id)：actor = 稳定归因身份
--（user id / key:<id> / system），不同 actor 同 key 不冲突。
CREATE TABLE IF NOT EXISTS idempotency_keys (
    project_id       TEXT NOT NULL,
    actor_id         TEXT NOT NULL,
    request_id       TEXT NOT NULL,
    fingerprint      TEXT NOT NULL,
    claim_token      TEXT NOT NULL,
    state            TEXT NOT NULL,
    response_payload JSONB,
    expires_at       TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, actor_id, request_id)
);

-- 惰性清理（写入时顺带删除过期行）走本索引。
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_expires_at ON idempotency_keys(expires_at);
