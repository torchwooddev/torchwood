-- 无项目头的支付/订阅/iOS 定位。写入路径在 PR6 才用；表可先建。
CREATE TABLE IF NOT EXISTS provider_resource_index (
    provider     TEXT NOT NULL,
    kind         TEXT NOT NULL,
    provider_ref TEXT NOT NULL,
    project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    PRIMARY KEY (provider, kind, provider_ref)
);
