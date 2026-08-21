package bunrepo

// 项目 schema 内系统资源最终表名。查询必须经 Scoped / ProjectTable，禁止未限定名。
const (
	userTable       = "users"
	sessionTable    = "sessions"
	identityTable   = "identities"
	groupTable      = "groups"
	membershipTable = "memberships"
	bucketTable     = "buckets"
	fileTable       = "files"
)
