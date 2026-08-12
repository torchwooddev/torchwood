import { listQuery, type HttpTransport } from "../http.js";
import type { ListParams, Membership, Team } from "../types.js";

export class ServerTeamsService {
  constructor(private readonly http: HttpTransport) {}

  async create(input: { name: string; permissions?: string[] }): Promise<Team> {
    return this.http.request<Team>("POST", "/v1/server/teams", {
      auth: "apiKey",
      body: input,
    });
  }

  async list(params?: ListParams): Promise<Team[]> {
    const res = await this.http.request<{ teams: Team[] }>("GET", "/v1/server/teams", {
      auth: "apiKey",
      query: listQuery(params),
    });
    return res.teams ?? [];
  }

  async get(id: string): Promise<Team> {
    return this.http.request<Team>("GET", `/v1/server/teams/${id}`, { auth: "apiKey" });
  }

  async getPrefs(id: string): Promise<Record<string, unknown>> {
    const res = await this.http.request<{ prefs: Record<string, unknown> }>(
      "GET",
      `/v1/server/teams/${id}/prefs`,
      { auth: "apiKey" }
    );
    return res.prefs ?? {};
  }

  async updatePrefs(id: string, prefs: Record<string, unknown>): Promise<Record<string, unknown>> {
    const res = await this.http.request<{ prefs: Record<string, unknown> }>(
      "PUT",
      `/v1/server/teams/${id}/prefs`,
      { auth: "apiKey", body: { id, prefs } }
    );
    return res.prefs ?? {};
  }

  async delete(id: string): Promise<void> {
    await this.http.request<void>("DELETE", `/v1/server/teams/${id}`, { auth: "apiKey" });
  }

  async createMembership(
    teamId: string,
    input: {
      email?: string;
      user_id?: string;
      name?: string;
      roles?: string[];
      status?: string;
    }
  ): Promise<Membership> {
    return this.http.request<Membership>("POST", `/v1/server/teams/${teamId}/memberships`, {
      auth: "apiKey",
      body: { team_id: teamId, ...input },
    });
  }

  async listMemberships(teamId: string, params?: ListParams): Promise<Membership[]> {
    const res = await this.http.request<{ memberships: Membership[] }>(
      "GET",
      `/v1/server/teams/${teamId}/memberships`,
      { auth: "apiKey", query: listQuery(params) }
    );
    return res.memberships ?? [];
  }

  async getMembership(teamId: string, membershipId: string): Promise<Membership> {
    return this.http.request<Membership>(
      "GET",
      `/v1/server/teams/${teamId}/memberships/${membershipId}`,
      { auth: "apiKey" }
    );
  }

  async updateMembership(
    teamId: string,
    membershipId: string,
    roles: string[]
  ): Promise<Membership> {
    return this.http.request<Membership>(
      "PATCH",
      `/v1/server/teams/${teamId}/memberships/${membershipId}`,
      {
        auth: "apiKey",
        body: { team_id: teamId, membership_id: membershipId, roles },
      }
    );
  }

  async updateMembershipStatus(
    teamId: string,
    membershipId: string,
    status: string
  ): Promise<Membership> {
    return this.http.request<Membership>(
      "PATCH",
      `/v1/server/teams/${teamId}/memberships/${membershipId}/status`,
      {
        auth: "apiKey",
        body: { team_id: teamId, membership_id: membershipId, status },
      }
    );
  }

  async deleteMembership(teamId: string, membershipId: string): Promise<void> {
    await this.http.request<void>(
      "DELETE",
      `/v1/server/teams/${teamId}/memberships/${membershipId}`,
      { auth: "apiKey" }
    );
  }
}
