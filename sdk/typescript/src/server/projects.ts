import { listQuery, type HttpTransport } from "../http.js";
import type { ListParams, Project } from "../types.js";

export class ProjectsService {
  constructor(private readonly http: HttpTransport) {}

  async list(params?: ListParams): Promise<Project[]> {
    const res = await this.http.request<{ projects: Project[] }>("GET", "/v1/server/projects", {
      auth: "apiKey",
      query: listQuery(params),
    });
    return res.projects ?? [];
  }

  async get(id: string): Promise<Project> {
    return this.http.request<Project>("GET", `/v1/server/projects/${id}`, { auth: "apiKey" });
  }

  async create(input: { name: string; description?: string }): Promise<Project> {
    return this.http.request<Project>("POST", "/v1/server/projects", {
      auth: "apiKey",
      body: input,
    });
  }

  async update(
    id: string,
    input: { name?: string; description?: string }
  ): Promise<Project> {
    return this.http.request<Project>("PATCH", `/v1/server/projects/${id}`, {
      auth: "apiKey",
      body: { id, ...input },
    });
  }
}
