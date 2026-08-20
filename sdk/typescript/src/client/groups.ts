import { listQuery, type HttpTransport } from "../http.js";
import type { ListParams, Membership, Group } from "../types.js";

export class ClientGroupsService {
  constructor(private readonly http: HttpTransport) {}

  async createGroup(name: string): Promise<Group> {
    return this.http.request<Group>("POST", "/v1/groups", { body: { name } });
  }

  async listGroups(params?: ListParams): Promise<Group[]> {
    const res = await this.http.request<{ groups: Group[] }>("GET", "/v1/groups", {
      query: listQuery(params),
    });
    return res.groups ?? [];
  }

  async getGroup(id: string): Promise<Group> {
    return this.http.request<Group>("GET", `/v1/groups/${id}`);
  }

  async deleteGroup(id: string): Promise<void> {
    await this.http.request<void>("DELETE", `/v1/groups/${id}`);
  }

  async createMembership(
    groupId: string,
    input: { email: string; name?: string; roles?: string[] }
  ): Promise<Membership> {
    return this.http.request<Membership>("POST", `/v1/groups/${groupId}/memberships`, {
      body: {
        group_id: groupId,
        email: input.email,
        name: input.name ?? "",
        roles: input.roles,
      },
    });
  }

  async listMemberships(groupId: string): Promise<Membership[]> {
    const res = await this.http.request<{ memberships: Membership[] }>(
      "GET",
      `/v1/groups/${groupId}/memberships`
    );
    return res.memberships ?? [];
  }

  async updateMembershipStatus(
    groupId: string,
    membershipId: string,
    status: "accepted" | "rejected"
  ): Promise<Membership> {
    return this.http.request<Membership>(
      "PATCH",
      `/v1/groups/${groupId}/memberships/${membershipId}/status`,
      {
        body: {
          group_id: groupId,
          membership_id: membershipId,
          status,
        },
      }
    );
  }

  async deleteMembership(groupId: string, membershipId: string): Promise<void> {
    await this.http.request<void>(
      "DELETE",
      `/v1/groups/${groupId}/memberships/${membershipId}`
    );
  }
}
