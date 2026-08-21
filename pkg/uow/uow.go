// Package uow 定义对外工作单元缝（S-4）。
//
// 调用方用 Runner.Run 包住需要同一提交的写。fn 仍接收 ctx，适配器可从
// ctx 读取连接；领域端口不得把驱动类型写进注释。
package uow

import "context"

// Runner 执行 fn：已在工作单元内则加入，否则开启新单元。
type Runner interface {
	Run(ctx context.Context, fn func(ctx context.Context) error) error
}
