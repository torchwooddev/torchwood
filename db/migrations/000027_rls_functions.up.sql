-- RLS 判定单源函数（redesign §3.2/§3.3/§4.3，阶段③包 C）：tw_can/tw_coll_allows/
-- tw_visible 装 public（集群级，随 catalog 同库）；宿主是每集合表的 policy 与
-- golden 测试矩阵（CI 锁语义，禁止 Go 侧等价实现）。
--
-- 语义锚点（= AllowsDocumentAccess 用户集合分支，只换执行点不动模型）：
--   tw_can = ACE 命中（read 仅 'typ:role'；create/update/delete 同时匹配
--            'write:role'——matchTypes 的 write 展开）∨ 空 _acl 回退集合级（B1）
--   tw_visible = tw_can('read') ∨ tw_can('update') ∨ tw_can('delete')
--           （"可写即可读"产品语义，§3.2 已决）；docSec=false 纯集合级
--            （AllowsDocumentAccess 的 !DocumentSecurity 分支忠实保留）
--   roles 缺省（NULL/''）→ 零角色可匹配 → 恒 false（fail-closed）

CREATE OR REPLACE FUNCTION public.tw_roles()
RETURNS text[]
LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT string_to_array(NULLIF(current_setting('app.roles', true), ''), chr(31))
$$;

CREATE OR REPLACE FUNCTION public.tw_can(acl text[], roles text[], typ text, coll_allows boolean)
RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT COALESCE(EXISTS (
               SELECT 1
               FROM unnest(acl) ace
               CROSS JOIN LATERAL unnest(roles) r
               WHERE ace = typ || ':' || r
                  OR (typ <> 'read' AND ace = 'write:' || r)
           ), false)
        OR COALESCE(cardinality(acl) = 0 AND cardinality(roles) > 0 AND coll_allows, false)
$$;

-- tw_coll_allows 是集合级权限判定（catalog permissions JSONB，type:role 对）：
-- write 展开与 tw_can 同源（typ∈create/update/delete 同时命中 'write'）。
CREATE OR REPLACE FUNCTION public.tw_coll_allows(perms jsonb, roles text[], typ text)
RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT COALESCE((
               SELECT bool_or(
                      ((p ->> 'type' = typ)
                          OR (typ <> 'read' AND p ->> 'type' = 'write'))
                      AND (p ->> 'role' = ANY (roles)))
               FROM jsonb_array_elements(perms) p
           ), false)
$$;

-- tw_visible 是 SELECT policy 的可见谓词（可写即可读）。docsec=false 时 ACE
-- 不参与（AllowsDocumentAccess 的集合级独占分支），集合级写权同样蕴含可见。
-- 空 _acl 快速路径先行短路（等价于三次 tw_can 的空回退析取），免每行函数
-- 调用——大集合全扫的相对基准由它压住（I1）。
CREATE OR REPLACE FUNCTION public.tw_visible(
    acl text[], roles text[], docsec boolean,
    coll_read boolean, coll_update boolean, coll_delete boolean)
RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT CASE
        WHEN docsec THEN
               COALESCE(cardinality(acl) = 0 AND cardinality(roles) > 0
                   AND (coll_read OR coll_update OR coll_delete), false)
            OR public.tw_can(acl, roles, 'read', coll_read)
            OR public.tw_can(acl, roles, 'update', coll_update)
            OR public.tw_can(acl, roles, 'delete', coll_delete)
        ELSE coll_read OR coll_update OR coll_delete
    END
$$;
