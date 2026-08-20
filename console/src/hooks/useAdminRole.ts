import { useQuery } from "@tanstack/react-query";
import { getCurrentAdmin } from "@/api/admins";

export type AdminRole = "owner" | "admin" | "member" | "viewer";

// 角色判定辅助（与后端 console admins 角色语义对齐）：
// - owner/admin：平台管理员，可执行全部操作（含项目级/DDL/API Key/环境变量等敏感写操作）。
// - member：可写业务资源（用户创建/编辑、用户组、存储、函数部署与执行、文档 CRUD），
//   不可做平台级写操作（projects/settings/api-keys/databases DDL/functions 变量）。
// - viewer：只读。
export function isPlatformAdmin(role: string | undefined): boolean {
  return role === "owner" || role === "admin";
}

// canWrite fail-closed（Round3 H5-2）：显式白名单 owner | admin | member；
// viewer 与未知角色（undefined/其他值）一律不可写——未加载出角色时
// 宁可不展示写入口，也不放行。
export function canWrite(role: string | undefined): boolean {
  return role === "owner" || role === "admin" || role === "member";
}

// useAdminRole 返回当前登录 admin 的角色（共享 ["console-admin-me"] 查询缓存，
// 与 admins/pages.tsx 的 getCurrentAdmin 用法一致）。
export function useAdminRole(): {
  role: AdminRole | undefined;
  isLoading: boolean;
} {
  const { data, isLoading } = useQuery({
    queryKey: ["console-admin-me"],
    queryFn: getCurrentAdmin,
    retry: 1,
    staleTime: 60_000,
  });
  return { role: data?.role as AdminRole | undefined, isLoading };
}
