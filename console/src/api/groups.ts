import { api } from "./client";
import type { ApiRequestConfig } from "./client";

export interface Group {
  id: string;
  name: string;
  total: number;
  permissions?: string[];
  created_at: string;
  updated_at: string;
}

export interface Membership {
  id: string;
  group_id: string;
  user_id: string;
  email: string;
  name: string;
  roles: string[];
  status: string;
  invited_at?: string;
  joined_at?: string;
  created_at: string;
  updated_at: string;
}

export async function listGroups(): Promise<Group[]> {
  const res = await api.get<{ groups: Group[] }>("/server/groups");
  return res.data.groups ?? [];
}

export async function getGroup(id: string): Promise<Group> {
  const res = await api.get<Group>(`/server/groups/${id}`);
  return res.data;
}

export async function createGroup(input: { name: string }): Promise<Group> {
  const res = await api.post<Group>("/server/groups", input);
  return res.data;
}

export async function deleteGroup(id: string, config?: ApiRequestConfig): Promise<void> {
  await api.delete(`/server/groups/${id}`, config);
}

export async function getGroupPrefs(id: string): Promise<Record<string, unknown>> {
  const res = await api.get<{ prefs: Record<string, unknown> }>(`/server/groups/${id}/prefs`);
  return res.data.prefs ?? {};
}

export async function updateGroupPrefs(
  id: string,
  prefs: Record<string, unknown>
): Promise<Record<string, unknown>> {
  const res = await api.put<{ prefs: Record<string, unknown> }>(`/server/groups/${id}/prefs`, {
    prefs,
  });
  return res.data.prefs ?? {};
}

export async function listMemberships(groupId: string): Promise<Membership[]> {
  const res = await api.get<{ memberships: Membership[] }>(
    `/server/groups/${groupId}/memberships`
  );
  return res.data.memberships ?? [];
}

export async function createMembership(
  groupId: string,
  input: {
    email?: string;
    user_id?: string;
    name?: string;
    roles?: string[];
    status?: string;
  }
): Promise<Membership> {
  const res = await api.post<Membership>(`/server/groups/${groupId}/memberships`, {
    group_id: groupId,
    ...input,
  });
  return res.data;
}

export async function updateMembership(
  groupId: string,
  membershipId: string,
  input: { roles: string[] }
): Promise<Membership> {
  const res = await api.patch<Membership>(
    `/server/groups/${groupId}/memberships/${membershipId}`,
    {
      group_id: groupId,
      membership_id: membershipId,
      roles: input.roles,
    }
  );
  return res.data;
}

export async function updateMembershipStatus(
  groupId: string,
  membershipId: string,
  status: string
): Promise<Membership> {
  const res = await api.patch<Membership>(
    `/server/groups/${groupId}/memberships/${membershipId}/status`,
    {
      group_id: groupId,
      membership_id: membershipId,
      status,
    }
  );
  return res.data;
}

export async function deleteMembership(
  groupId: string,
  membershipId: string,
  config?: ApiRequestConfig
): Promise<void> {
  await api.delete(`/server/groups/${groupId}/memberships/${membershipId}`, config);
}
