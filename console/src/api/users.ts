import { api } from "./client";

export interface User {
  id: string;
  email: string;
  name: string;
  status: string;
  email_verified: boolean;
  labels?: string[];
  prefs?: Record<string, unknown>;
  phone?: string;
  created_at: string;
  updated_at: string;
}

export interface UserSession {
  id: string;
  user_id: string;
  provider: string;
  user_agent: string;
  ip: string;
  expire_at?: string;
  created_at: string;
}

export interface TokenBundle {
  access_token: string;
  refresh_token: string;
  expires_at: number;
}

export interface ListUsersResponse {
  users: User[];
  meta?: { total_count?: number; page_size?: number };
}

export async function listUsers(): Promise<User[]> {
  const res = await api.get<ListUsersResponse>("/server/users");
  return res.data.users ?? [];
}

export async function getUser(id: string): Promise<User> {
  const res = await api.get<User>(`/server/users/${id}`);
  return res.data;
}

export async function createUser(input: {
  email: string;
  password: string;
  name?: string;
  status?: string;
  labels?: string[];
  prefs?: Record<string, unknown>;
}): Promise<User> {
  const res = await api.post<User>("/server/users", {
    ...input,
    labels: input.labels ? { values: input.labels } : undefined,
  });
  return res.data;
}

export async function updateUser(
  id: string,
  input: {
    name?: string;
    email?: string;
    status?: string;
    email_verified?: boolean;
    labels?: string[];
    prefs?: Record<string, unknown>;
  }
): Promise<User> {
  const res = await api.patch<User>(`/server/users/${id}`, {
    ...input,
    labels: input.labels ? { values: input.labels } : undefined,
  });
  return res.data;
}

export async function updateUserPassword(id: string, password: string): Promise<User> {
  const res = await api.patch<User>(`/server/users/${id}/password`, { password });
  return res.data;
}

export async function deleteUser(id: string): Promise<void> {
  await api.delete(`/server/users/${id}`);
}

export async function listUserSessions(id: string): Promise<UserSession[]> {
  const res = await api.get<{ sessions: UserSession[] }>(`/server/users/${id}/sessions`);
  return res.data.sessions ?? [];
}

export async function deleteUserSession(id: string, sessionId: string): Promise<void> {
  await api.delete(`/server/users/${id}/sessions/${sessionId}`);
}

export async function createUserToken(id: string): Promise<TokenBundle> {
  const res = await api.post<{ tokens: TokenBundle }>(`/server/users/${id}/tokens`);
  return res.data.tokens;
}
