import { listQuery, type HttpTransport } from "../http.js";
import type { ListParams } from "../types.js";

export interface UsageMetric {
  metric: string;
  value: string;
}

export interface Usage {
  project_id: string;
  period_start?: string;
  period_end?: string;
  metrics?: UsageMetric[];
}

export interface UsageRollup {
  id: string;
  project_id: string;
  metric: string;
  period_start?: string;
  value: string;
}

export interface BillingStatement {
  id: string;
  project_id: string;
  period_start?: string;
  period_end?: string;
  status: string;
  details?: Record<string, unknown>;
  created_at?: string;
  finalized_at?: string;
}

export class BillingService {
  constructor(private readonly http: HttpTransport) {}

  async getUsage(params?: { metric?: string; period_start?: string; period_end?: string }): Promise<Usage> {
    return this.http.request<Usage>("GET", "/v1/server/billing/usage", {
      auth: "apiKey",
      query: {
        metric: params?.metric,
        period_start: params?.period_start,
        period_end: params?.period_end,
      },
    });
  }

  async listRollups(
    params?: ListParams & { metric?: string; period_start?: string; period_end?: string }
  ): Promise<UsageRollup[]> {
    const res = await this.http.request<{ rollups: UsageRollup[] }>(
      "GET",
      "/v1/server/billing/rollups",
      {
        auth: "apiKey",
        query: {
          ...listQuery(params),
          metric: params?.metric,
          period_start: params?.period_start,
          period_end: params?.period_end,
        },
      }
    );
    return res.rollups ?? [];
  }

  async listStatements(params?: ListParams): Promise<BillingStatement[]> {
    const res = await this.http.request<{ statements: BillingStatement[] }>(
      "GET",
      "/v1/server/billing/statements",
      { auth: "apiKey", query: listQuery(params) }
    );
    return res.statements ?? [];
  }
}
