import type { HttpTransport } from "../http.js";

export class HealthService {
  constructor(private readonly http: HttpTransport) {}

  async check(): Promise<{ status: string }> {
    return this.http.request<{ status: string }>("GET", "/v1/health", { auth: "none" });
  }

  async getVersion(): Promise<{ version: string; commit: string; date: string }> {
    return this.http.request("GET", "/v1/server/health/version", { auth: "none" });
  }
}
