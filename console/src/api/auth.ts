import { api, refreshAuthTokenSingleFlight } from "./client";
import type { ApiRequestConfig } from "./client";

export interface LoginInput {
  email: string;
  password: string;
}

// login 成功后会话凭证由服务端通过 HttpOnly cookie 下发，响应体中的 token
// 仅供直连 gRPC 客户端使用，浏览器端不保存。
// __skipToast：登录页自行渲染错误（见 routes/Login.tsx），避免全局 toast 双显。
export async function login(input: LoginInput): Promise<void> {
  const config: ApiRequestConfig = { __skipToast: true };
  await api.post("/console/auth/sign-in", input, config);
}

// refreshSession 用 refresh cookie 探测/续期会话，成功即处于已登录状态。
export async function refreshSession(): Promise<void> {
  return refreshAuthTokenSingleFlight();
}

// logout revokes the admin token pair server-side and clears the session
// cookies (skipped from the 401 retry flow). Errors propagate to the caller
// so the UI can warn when the session may still be valid.
export async function logout(): Promise<void> {
  const config: ApiRequestConfig = { __skipAuthRetry: true };
  await api.post("/console/auth/sign-out", {}, config);
}

export interface SetupStatus {
  needs_setup: boolean;
  // 部署方配置了 TORCHWOOD_SECURITY_SETUP_TOKEN 时为 true（登录页需
  // 展示 setup token 输入框）。
  setup_token_required: boolean;
}

// getSetupStatus 查询是否尚未初始化（needs_setup=true 时登录页切换为
// 初始化设置表单）。__skipToast：探测失败由登录页内联展示错误 + 重试。
export async function getSetupStatus(): Promise<SetupStatus> {
  const config: ApiRequestConfig = { __skipToast: true };
  const res = await api.get<SetupStatus>("/console/auth/setup-status", config);
  return res.data;
}

export interface SignUpInput {
  email: string;
  password: string;
  // 服务端配置了引导令牌时必填，否则注册被拒绝。
  setup_token?: string;
  project_id: string;
  database_id: string;
}

export interface SignUpResult {
  admin: { id: string; email: string; role: string };
}

// signUp 注册首个管理员（owner）并创建指定 project / database。
// 不生成 API Key。成功后服务端通过 HttpOnly cookie 下发会话凭证。
// __skipToast：初始化页自行渲染错误，避免全局 toast 双显。
export async function signUp(input: SignUpInput): Promise<SignUpResult> {
  const config: ApiRequestConfig = { __skipToast: true };
  const res = await api.post<SignUpResult>("/console/auth/sign-up", input, config);
  return res.data;
}
