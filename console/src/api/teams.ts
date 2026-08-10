import { api } from "./client";

export interface Team {
  id: string;
  name: string;
  total: number;
  permissions?: string[];
  created_at: string;
  updated_at: string;
}

export interface Membership {
  id: string;
  team_id: string;
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

export async function listTeams(): Promise<Team[]> {
  const res = await api.get<{ teams: Team[] }>("/server/teams");
  return res.data.teams ?? [];
}

export async function getTeam(id: string): Promise<Team> {
  const res = await api.get<Team>(`/server/teams/${id}`);
  return res.data;
}

export async function createTeam(input: { name: string }): Promise<Team> {
  const res = await api.post<Team>("/server/teams", input);
  return res.data;
}

export async function deleteTeam(id: string): Promise<void> {
  await api.delete(`/server/teams/${id}`);
}

export async function getTeamPrefs(id: string): Promise<Record<string, unknown>> {
  const res = await api.get<{ prefs: Record<string, unknown> }>(`/server/teams/${id}/prefs`);
  return res.data.prefs ?? {};
}

export async function updateTeamPrefs(
  id: string,
  prefs: Record<string, unknown>
): Promise<Record<string, unknown>> {
  const res = await api.put<{ prefs: Record<string, unknown> }>(`/server/teams/${id}/prefs`, {
    prefs,
  });
  return res.data.prefs ?? {};
}

export async function listMemberships(teamId: string): Promise<Membership[]> {
  const res = await api.get<{ memberships: Membership[] }>(
    `/server/teams/${teamId}/memberships`
  );
  return res.data.memberships ?? [];
}

export async function createMembership(
  teamId: string,
  input: {
    email?: string;
    user_id?: string;
    name?: string;
    roles?: string[];
    status?: string;
  }
): Promise<Membership> {
  const res = await api.post<Membership>(`/server/teams/${teamId}/memberships`, {
    team_id: teamId,
    ...input,
  });
  return res.data;
}

export async function updateMembership(
  teamId: string,
  membershipId: string,
  input: { roles: string[] }
): Promise<Membership> {
  const res = await api.patch<Membership>(
    `/server/teams/${teamId}/memberships/${membershipId}`,
    {
      team_id: teamId,
      membership_id: membershipId,
      roles: input.roles,
    }
  );
  return res.data;
}

export async function updateMembershipStatus(
  teamId: string,
  membershipId: string,
  status: string
): Promise<Membership> {
  const res = await api.patch<Membership>(
    `/server/teams/${teamId}/memberships/${membershipId}/status`,
    {
      team_id: teamId,
      membership_id: membershipId,
      status,
    }
  );
  return res.data;
}

export async function deleteMembership(teamId: string, membershipId: string): Promise<void> {
  await api.delete(`/server/teams/${teamId}/memberships/${membershipId}`);
}
