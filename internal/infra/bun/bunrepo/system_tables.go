package bunrepo

// 项目 schema 内 User / Session 物理表名。查询必须经 Scoped / ProjectTable，禁止未限定名。
const (
	userTable    = "sys_users"
	sessionTable = "sys_sessions"
)
