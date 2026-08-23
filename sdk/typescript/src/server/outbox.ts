import { listQuery, type HttpTransport } from "../http.js";
import type { ListParams } from "../types.js";

export interface DeadLetter {
  event_id: string;
  project_id: string;
  topic: string;
  channel: string;
  payload: Uint8Array;
  attempts: number;
  last_error: string;
  created_at: string;
}

export class OutboxService {
  constructor(private readonly http: HttpTransport) {}

  async listDeadLetters(projectId: string, params?: ListParams): Promise<DeadLetter[]> {
    const res = await this.http.request<{ dead_letters: DeadLetter[] }>("GET", "/v1/server/outbox/dead-letters", {
      auth: "apiKey",
      query: { project_id: projectId, ...listQuery(params) },
    });
    return res.dead_letters ?? [];
  }

  async replayDeadLetter(eventId: string, projectId: string): Promise<{ event_id: string }> {
    return this.http.request<{ event_id: string }>("POST", `/v1/server/outbox/dead-letters/${eventId}:replay`, {
      auth: "apiKey",
      body: { event_id: eventId, project_id: projectId },
    });
  }
}
