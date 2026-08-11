import { api } from "./client";

export interface Admin {
  id: string;
  email: string;
  role: string;
  created_at?: string;
  updated_at?: string;
}

export interface ListAdminsResponse {
  admins: Admin[];
}

export const ADMIN_ROLES = ["owner", "admin", "member", "viewer"] as const;

export async function getCurrentAdmin(): Promise<Admin> {
  const res = await api.get<Admin>("/console/admins/me");
  return res.data;
}

export async function listAdmins(): Promise<Admin[]> {
  const res = await api.get<ListAdminsResponse>("/console/admins");
  return res.data.admins ?? [];
}

export async function createAdmin(input: {
  email: string;
  password: string;
  role: string;
}): Promise<Admin> {
  const res = await api.post<Admin>("/console/admins", input);
  return res.data;
}

export async function updateAdmin(
  id: string,
  input: { role?: string; password?: string }
): Promise<Admin> {
  const res = await api.patch<Admin>(
    `/console/admins/${encodeURIComponent(id)}`,
    input
  );
  return res.data;
}

export async function deleteAdmin(id: string): Promise<void> {
  await api.delete(`/console/admins/${encodeURIComponent(id)}`);
}
