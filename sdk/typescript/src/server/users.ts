import { listQuery, type HttpTransport } from "../http.js";
import type { ListParams, Session, TokenBundle, User } from "../types.js";

export class UsersService {
  constructor(private readonly http: HttpTransport) {}

  async create(input: {
    email: string;
    password: string;
    name?: string;
    status?: string;
    labels?: Record<string, unknown>;
    prefs?: Record<string, unknown>;
  }): Promise<User> {
    return this.http.request<User>("POST", "/v1/server/users", {
      auth: "apiKey",
      body: input,
    });
  }

  async list(params?: ListParams): Promise<User[]> {
    const res = await this.http.request<{ users: User[] }>("GET", "/v1/server/users", {
      auth: "apiKey",
      query: listQuery(params),
    });
    return res.users ?? [];
  }

  async get(id: string): Promise<User> {
    return this.http.request<User>("GET", `/v1/server/users/${id}`, { auth: "apiKey" });
  }

  async update(
    id: string,
    input: {
      name?: string;
      email?: string;
      status?: string;
      email_verified?: boolean;
      labels?: Record<string, unknown>;
      prefs?: Record<string, unknown>;
    }
  ): Promise<User> {
    return this.http.request<User>("PATCH", `/v1/server/users/${id}`, {
      auth: "apiKey",
      body: input,
    });
  }

  async updatePassword(id: string, password: string): Promise<User> {
    return this.http.request<User>("PATCH", `/v1/server/users/${id}/password`, {
      auth: "apiKey",
      body: { password },
    });
  }

  async delete(id: string): Promise<void> {
    await this.http.request<void>("DELETE", `/v1/server/users/${id}`, { auth: "apiKey" });
  }

  async listSessions(id: string): Promise<Session[]> {
    const res = await this.http.request<{ sessions: Session[] }>(
      "GET",
      `/v1/server/users/${id}/sessions`,
      { auth: "apiKey" }
    );
    return res.sessions ?? [];
  }

  async deleteSession(id: string, sessionId: string): Promise<void> {
    await this.http.request<void>("DELETE", `/v1/server/users/${id}/sessions/${sessionId}`, {
      auth: "apiKey",
    });
  }

  async createToken(id: string): Promise<TokenBundle> {
    const res = await this.http.request<{ tokens: TokenBundle }>(
      "POST",
      `/v1/server/users/${id}/tokens`,
      { auth: "apiKey" }
    );
    return res.tokens;
  }
}
