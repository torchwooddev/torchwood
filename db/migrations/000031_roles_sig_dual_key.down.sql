-- 000031 逆序回滚：恢复 000029（R16）单钥形态——tw_sig_match 回单钥
-- LATERAL 版（000029 原文逐字），tw_secrets 收敛为每 purpose 一行（current
-- 优先、其下取最新）后撤 is_current 列与部分唯一索引、恢复 purpose 主键。
-- partial index 随 is_current 列删除（谓词依赖连带 DROP）。

CREATE OR REPLACE FUNCTION public.tw_sig_match()
RETURNS boolean
LANGUAGE sql STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT COALESCE((
        SELECT sig.exp ~ '^[0-9]+$'
           AND (sig.exp)::bigint >= (EXTRACT(epoch FROM now()))::bigint
           AND encode(public.hmac(
                   COALESCE(current_setting('app.tenant', true), '') || '|' ||
                   current_setting('app.roles', true) || '|' || sig.exp,
                   k.key_hex, 'sha256'), 'hex') = sig.mac
        FROM (SELECT split_part(current_setting('app.roles_sig', true), '|', 1) AS exp,
                     split_part(current_setting('app.roles_sig', true), '|', 2) AS mac) sig
        LEFT JOIN LATERAL (
            SELECT key_hex FROM public.tw_secrets WHERE purpose = 'tw-roles-guc-v1' LIMIT 1
        ) k ON true
    ), false)
$$;

-- 双钥行收敛：current 行优先保留；无 current 时保留 (updated_at, key_hex)
-- 字典序最大的 previous 行（与 up 侧 Go 平移逻辑的保留规则一致）。
DELETE FROM public.tw_secrets s
WHERE EXISTS (
    SELECT 1 FROM public.tw_secrets o
    WHERE o.purpose = s.purpose AND o.key_hex <> s.key_hex
      AND ( (o.is_current AND NOT s.is_current)
         OR (o.is_current = s.is_current AND (o.updated_at, o.key_hex) > (s.updated_at, s.key_hex)) )
);

ALTER TABLE public.tw_secrets
    DROP CONSTRAINT tw_secrets_pkey,
    DROP COLUMN is_current,
    ADD PRIMARY KEY (purpose);
