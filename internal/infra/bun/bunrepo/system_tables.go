package bunrepo

// 项目 schema 内系统资源 staging 表名。查询必须经 Scoped / ProjectTable，禁止未限定名。
const (
	userTable       = "sys_users"
	sessionTable    = "sys_sessions"
	identityTable   = "sys_identities"
	groupTable      = "sys_groups"
	membershipTable = "sys_memberships"
	bucketTable     = "sys_buckets"
	fileTable       = "sys_files"
)
