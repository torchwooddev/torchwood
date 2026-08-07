-- 安全修复 P0-1：收紧存量项目 users 系统集合的 create 权限。
-- users 集合此前 collection 级权限为 create:any，配合客户端文档 API 可被任意
-- 认证用户直写用户文档（伪造用户/自举 email_verified/labels）。
-- 收窄为 create:keys 后，仅 API key / admin 可创建用户文档；
-- 客户端注册路径（SignUp / OTP / 匿名）均以 SystemPrincipal 写入，不受影响。
UPDATE document_collections
SET permissions = array_replace(permissions, 'create:any', 'create:keys')
WHERE database_id = 'default'
  AND id = 'users'
  AND 'create:any' = ANY(permissions);
