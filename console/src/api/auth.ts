import { api, refreshAuthTokenSingleFlight } from "./client";
import type { ApiRequestConfig } from "./client";

export interface LoginInput {
  email: string;
  password: string;
}

// login 成功后会话凭证由服务端通过 HttpOnly cookie 下发，响应体中的 token
// 仅供直连 gRPC 客户端使用，浏览器端不保存。
export async function login(input: LoginInput): Promise<void> {
  await api.post("/console/auth/sign-in", input);
}

// refreshSession 用 refresh cookie 探测/续期会话，成功即处于已登录状态。
export async function refreshSession(): Promise<void> {
  return refreshAuthTokenSingleFlight();
}

// logout revokes the admin token pair server-side and clears the session
// cookies (best-effort: the call is skipped from the 401 retry flow).
export async function logout(): Promise<void> {
  try {
    const config: ApiRequestConfig = { __skipAuthRetry: true };
    await api.post("/console/auth/sign-out", {}, config);
  } catch {
    // Ignore: sign-out is best-effort.
  }
}

// getSetupStatus 查询是否尚未初始化（needs_setup=true 时登录页切换为
// 初始化设置表单）。
export async function getSetupStatus(): Promise<boolean> {
  const res = await api.get<{ needs_setup: boolean }>("/console/auth/setup-status");
  return res.data.needs_setup;
}

export interface SignUpInput {
  email: string;
  password: string;
}

export interface SignUpResult {
  admin: { id: string; email: string; role: string };
  // default_api_key_secret 仅此一次返回，前端只展示不持久化。
  default_api_key_secret: string;
}

// signUp 注册首个管理员（owner）。成功后服务端通过 HttpOnly cookie 下发
// 会话凭证，浏览器端无需再次登录。
export async function signUp(input: SignUpInput): Promise<SignUpResult> {
  const res = await api.post<SignUpResult>("/console/auth/sign-up", input);
  return res.data;
}
