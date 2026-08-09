import { api } from "./client";

export interface ConsoleAdmin {
  id: string;
  email: string;
  role: string;
  created_at?: string;
  updated_at?: string;
}

export interface ListAdminsResponse {
  admins: ConsoleAdmin[];
}

export const ADMIN_ROLES = ["owner", "admin", "member", "viewer"] as const;

export async function getCurrentAdmin(): Promise<ConsoleAdmin> {
  const res = await api.get<ConsoleAdmin>("/console/admins/me");
  return res.data;
}

export async function listAdmins(): Promise<ConsoleAdmin[]> {
  const res = await api.get<ListAdminsResponse>("/console/admins");
  return res.data.admins ?? [];
}

export async function createAdmin(input: {
  email: string;
  password: string;
  role: string;
}): Promise<ConsoleAdmin> {
  const res = await api.post<ConsoleAdmin>("/console/admins", input);
  return res.data;
}

export async function updateAdmin(
  id: string,
  input: { role?: string; password?: string }
): Promise<ConsoleAdmin> {
  const res = await api.patch<ConsoleAdmin>(
    `/console/admins/${encodeURIComponent(id)}`,
    input
  );
  return res.data;
}

export async function deleteAdmin(id: string): Promise<void> {
  await api.delete(`/console/admins/${encodeURIComponent(id)}`);
}
