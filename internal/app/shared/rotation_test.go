package shared

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProjectRotation 锁设计 §7「下一 tick 从下一 cursor 继续，避免永远饿死
// 队尾项目」：预算提前结束于 idx 时下一轮从 idx 起步；完整遍历后回头部；
// 项目数收缩（删项目）导致游标越界时回头部。
func TestProjectRotation(t *testing.T) {
	var r ProjectRotation

	// 初始：从头部开始。
	require.Equal(t, 0, r.Start(3))

	// 提前结束于 idx=2（该项目未处理）：下一 tick 从 2 开始。
	r.ResumeAt(2)
	require.Equal(t, 2, r.Start(3))

	// 完整遍历一轮：回头部。
	r.Complete()
	require.Equal(t, 0, r.Start(3))

	// 环形：提前结束于最后一个（idx=2），且下一轮项目数不变 → 从 2 起步；
	// 再提前结束于 2 → 仍从 2 起步（不越界）。
	r.ResumeAt(2)
	require.Equal(t, 2, r.Start(3))
	r.ResumeAt(2)
	require.Equal(t, 2, r.Start(3))

	// ResumeAt 负值防御：回头部。
	r.ResumeAt(-1)
	require.Equal(t, 0, r.Start(3))

	// 项目数收缩：游标 2 在 n=2 时越界 → 回头部。
	r2 := ProjectRotation{}
	r2.ResumeAt(2)
	require.Equal(t, 0, r2.Start(2))

	// 空列表：恒 0。
	r3 := ProjectRotation{}
	r3.ResumeAt(5)
	require.Equal(t, 0, r3.Start(0))
	require.Equal(t, 0, r3.Start(-1))
}
