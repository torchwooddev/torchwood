package functions

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// G8-2 掩码约定：SetVariables 请求中值等于 secretMask 的 key 保留旧值不覆盖，
// 响应返回掩码视图（真实值仅请求中可见一次）。
func TestSetVariables_PreservesMaskedExistingValues(t *testing.T) {
	repo := newMockRepo()
	fn := seedReadyFunction(repo, "p1", "fn_1", true, 15)
	require.NoError(t, repo.SetVariables(context.Background(), fn.ProjectID, fn.ID, map[string]string{
		"A": "secret1",
		"B": "",
	}))
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	got, err := uc.SetVariables(platformAdminCtx(), "p1", "fn_1", map[string]string{
		"A": secretMask,
		"B": "new-b",
		"C": "fresh",
	})
	require.NoError(t, err)

	// 掩码项 A 保留旧值；B/C 按新值写入（全量替换语义）。
	raw, err := repo.GetVariables(context.Background(), "p1", "fn_1")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"A": "secret1", "B": "new-b", "C": "fresh"}, raw)

	// 响应为掩码视图，不回显真实值。
	require.Equal(t, "******", got["A"])
	require.Equal(t, "******", got["B"])
	require.Equal(t, "******", got["C"])
}

// 掩码项对应的 key 不存在时不创建；未提及的 key 仍按全量替换语义删除。
func TestSetVariables_MaskedUnknownKeyNotCreated(t *testing.T) {
	repo := newMockRepo()
	fn := seedReadyFunction(repo, "p1", "fn_1", true, 15)
	require.NoError(t, repo.SetVariables(context.Background(), fn.ProjectID, fn.ID, map[string]string{
		"A": "x",
		"B": "y",
	}))
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	_, err := uc.SetVariables(platformAdminCtx(), "p1", "fn_1", map[string]string{
		"A": secretMask,
		"C": secretMask,
	})
	require.NoError(t, err)

	raw, err := repo.GetVariables(context.Background(), "p1", "fn_1")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"A": "x"}, raw)
}

// 无掩码项时保持原有全量替换语义。
func TestSetVariables_FullReplaceStillWorks(t *testing.T) {
	repo := newMockRepo()
	fn := seedReadyFunction(repo, "p1", "fn_1", true, 15)
	require.NoError(t, repo.SetVariables(context.Background(), fn.ProjectID, fn.ID, map[string]string{"A": "old"}))
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	_, err := uc.SetVariables(platformAdminCtx(), "p1", "fn_1", map[string]string{"B": "b"})
	require.NoError(t, err)

	raw, err := repo.GetVariables(context.Background(), "p1", "fn_1")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"B": "b"}, raw)
}

// 掩码保留的旧值与新值合并后超限 → InvalidArgument（预算按合并后结果计算）。
func TestSetVariables_MergedSizeBudget(t *testing.T) {
	repo := newMockRepo()
	fn := seedReadyFunction(repo, "p1", "fn_1", true, 15)
	big := strings.Repeat("v", 30<<10)
	require.NoError(t, repo.SetVariables(context.Background(), fn.ProjectID, fn.ID, map[string]string{"A": big}))
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	_, err := uc.SetVariables(platformAdminCtx(), "p1", "fn_1", map[string]string{
		"A": secretMask,
		"B": strings.Repeat("w", 10<<10),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "exceed maximum total size")
}
