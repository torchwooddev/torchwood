package functions

// RuntimeInfo 描述一个受支持的函数运行时。
type RuntimeInfo struct {
	ID         string // e.g. node-18.0
	Name       string // 展示名
	Entrypoint string // e.g. "index.main"
}

// SpecificationInfo 描述一个受支持的资源规格。
type SpecificationInfo struct {
	ID     string // e.g. shared-1x
	CPU    string // docker --cpus 值
	Memory string // docker --memory 值
}
