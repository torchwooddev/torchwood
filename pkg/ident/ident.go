package ident

import (
	"regexp"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// MaxSchemaResourceIDLen 是 project.id / database.id 的最大长度。
	MaxSchemaResourceIDLen = 28
	// SchemaPrefix 是动态文档 PostgreSQL schema 的固定前缀。
	SchemaPrefix = "tw_"
)

const errSchemaResourceID = "id must match ^[a-z][a-z0-9]{0,27}$"

// 小写字母开头，后接 0–27 个小写字母或数字。合计最长 28。
var schemaResourceIDRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,27}$`)

var schemaNameRe = regexp.MustCompile(`^tw_[a-z][a-z0-9]{0,27}_[a-z][a-z0-9]{0,27}$`)

// ValidateSchemaResourceID 校验 project.id / database.id。
func ValidateSchemaResourceID(id string) error {
	if id == "" || !schemaResourceIDRe.MatchString(id) {
		return status.Error(codes.InvalidArgument, errSchemaResourceID)
	}
	return nil
}

// SchemaName 拼出 tw_{project.id}_{database.id}。两端非法时返回 error，不拼接。
func SchemaName(projectID, databaseID string) (string, error) {
	if err := ValidateSchemaResourceID(projectID); err != nil {
		return "", err
	}
	if err := ValidateSchemaResourceID(databaseID); err != nil {
		return "", err
	}
	name := SchemaPrefix + projectID + "_" + databaseID
	if !schemaNameRe.MatchString(name) {
		return "", status.Error(codes.InvalidArgument, errSchemaResourceID)
	}
	return name, nil
}
