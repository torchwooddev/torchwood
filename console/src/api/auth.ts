import {
  api,
  setAuthToken,
  setRefreshToken,
  refreshAuthTokenSingleFlight,
} from "./client";
import type { ApiRequestConfig } from "./client";

export interface LoginInput {
  email: string;
  password: string;
}

export interface LoginResponse {
  access_token: string;
  expires_at: string;
  refresh_token?: string;
}

export async function login(input: LoginInput): Promise<string> {
  const res = await api.post<LoginResponse>("/console/auth/sign-in", input);
  setAuthToken(res.data.access_token);
  if (res.data.refresh_token) {
    setRefreshToken(res.data.refresh_token);
  }
  return res.data.access_token;
}

export async function refreshAuthToken(): Promise<string> {
  return refreshAuthTokenSingleFlight();
}

// logout revokes the admin token pair server-side (best-effort: local cleanup
// proceeds even when the request fails), then clears local credentials.
export async function logout(): Promise<void> {
  try {
    const config: ApiRequestConfig = { __skipAuthRetry: true };
    await api.post("/console/auth/sign-out", {}, config);
  } catch {
    // Ignore: sign-out is best-effort, local cleanup must always run.
  } finally {
    setAuthToken(null);
    setRefreshToken(null);
  }
}
