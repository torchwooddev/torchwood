import { listQuery, type HttpTransport } from "../http.js";
import type { ListParams, Membership, Group } from "../types.js";

export class ServerGroupsService {
  constructor(private readonly http: HttpTransport) {}

  async create(input: { name: string; permissions?: string[] }): Promise<Group> {
    return this.http.request<Group>("POST", "/v1/server/groups", {
      auth: "apiKey",
      body: input,
    });
  }

  async list(params?: ListParams): Promise<Group[]> {
    const res = await this.http.request<{ groups: Group[] }>("GET", "/v1/server/groups", {
      auth: "apiKey",
      query: listQuery(params),
    });
    return res.groups ?? [];
  }

  async get(id: string): Promise<Group> {
    return this.http.request<Group>("GET", `/v1/server/groups/${id}`, { auth: "apiKey" });
  }

  async getPrefs(id: string): Promise<Record<string, unknown>> {
    const res = await this.http.request<{ prefs: Record<string, unknown> }>(
      "GET",
      `/v1/server/groups/${id}/prefs`,
      { auth: "apiKey" }
    );
    return res.prefs ?? {};
  }

  async updatePrefs(id: string, prefs: Record<string, unknown>): Promise<Record<string, unknown>> {
    const res = await this.http.request<{ prefs: Record<string, unknown> }>(
      "PUT",
      `/v1/server/groups/${id}/prefs`,
      { auth: "apiKey", body: { id, prefs } }
    );
    return res.prefs ?? {};
  }

  async delete(id: string): Promise<void> {
    await this.http.request<void>("DELETE", `/v1/server/groups/${id}`, { auth: "apiKey" });
  }

  async createMembership(
    groupId: string,
    input: {
      email?: string;
      user_id?: string;
      name?: string;
      roles?: string[];
      status?: string;
    }
  ): Promise<Membership> {
    return this.http.request<Membership>("POST", `/v1/server/groups/${groupId}/memberships`, {
      auth: "apiKey",
      body: { group_id: groupId, ...input },
    });
  }

  async listMemberships(groupId: string, params?: ListParams): Promise<Membership[]> {
    const res = await this.http.request<{ memberships: Membership[] }>(
      "GET",
      `/v1/server/groups/${groupId}/memberships`,
      { auth: "apiKey", query: listQuery(params) }
    );
    return res.memberships ?? [];
  }

  async getMembership(groupId: string, membershipId: string): Promise<Membership> {
    return this.http.request<Membership>(
      "GET",
      `/v1/server/groups/${groupId}/memberships/${membershipId}`,
      { auth: "apiKey" }
    );
  }

  async updateMembership(
    groupId: string,
    membershipId: string,
    roles: string[]
  ): Promise<Membership> {
    return this.http.request<Membership>(
      "PATCH",
      `/v1/server/groups/${groupId}/memberships/${membershipId}`,
      {
        auth: "apiKey",
        body: { group_id: groupId, membership_id: membershipId, roles },
      }
    );
  }

  async updateMembershipStatus(
    groupId: string,
    membershipId: string,
    status: string
  ): Promise<Membership> {
    return this.http.request<Membership>(
      "PATCH",
      `/v1/server/groups/${groupId}/memberships/${membershipId}/status`,
      {
        auth: "apiKey",
        body: { group_id: groupId, membership_id: membershipId, status },
      }
    );
  }

  async deleteMembership(groupId: string, membershipId: string): Promise<void> {
    await this.http.request<void>(
      "DELETE",
      `/v1/server/groups/${groupId}/memberships/${membershipId}`,
      { auth: "apiKey" }
    );
  }
}
