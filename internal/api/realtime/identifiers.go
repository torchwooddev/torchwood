package realtime

import "regexp"

// identifierRe 与 internal/app/server/databases.go 的 identifier 校验一致
// （databaseId/collectionId 不含 "."）。
var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// docIDRe 与 internal/infra/documentdb/postgres.go 的 docID 校验一致
// （可含 "." ":" "-"，最长 64）。
var docIDRe = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,64}$`)
