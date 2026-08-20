package bunrepo

import (
	"github.com/torchwooddev/torchwood/pkg/ident"
	"github.com/uptrace/bun"
)

// ProjectTable 返回项目数据面 schema 的 bun.Ident 与 ModelTableExpr 模板
// （"?." + table + " AS " + alias）。禁止未限定表名。
func ProjectTable(projectID, table, alias string) (bun.Ident, string, error) {
	schema, err := ident.ProjectSchemaName(projectID)
	if err != nil {
		return bun.Ident(""), "", err
	}
	expr := "?." + table
	if alias != "" {
		expr += " AS " + alias
	}
	return bun.Ident(schema), expr, nil
}
