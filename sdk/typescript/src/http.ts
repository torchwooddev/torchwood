import { TorchwoodError, parseErrorResponse } from "./errors.js";

export type AuthMode = "apiKey" | "user" | "none";

export interface TorchwoodConfig {
  endpoint: string;
  projectId: string;
  apiKey?: string;
  accessToken?: string;
  fetch?: typeof fetch;
}

export interface RequestOptions {
  auth?: AuthMode;
  query?: Record<string, string | number | string[] | undefined>;
  body?: unknown;
}

export class HttpTransport {
  private endpoint: string;
  private projectId: string;
  private apiKey?: string;
  private accessToken?: string;
  private fetchImpl: typeof fetch;

  constructor(config: TorchwoodConfig) {
    this.endpoint = config.endpoint.replace(/\/+$/, "");
    this.projectId = config.projectId;
    this.apiKey = config.apiKey;
    this.accessToken = config.accessToken;
    this.fetchImpl = config.fetch ?? ((input, init) => globalThis.fetch(input, init));
  }

  getProjectId(): string {
    return this.projectId;
  }

  getAccessToken(): string | undefined {
    return this.accessToken;
  }

  setAccessToken(token: string | undefined): void {
    this.accessToken = token;
  }

  setApiKey(key: string | undefined): void {
    this.apiKey = key;
  }

  async request<T>(method: string, path: string, options: RequestOptions = {}): Promise<T> {
    const url = new URL(`${this.endpoint}${path.startsWith("/") ? path : `/${path}`}`);
    if (options.query) {
      for (const [key, value] of Object.entries(options.query)) {
        if (value === undefined) continue;
        if (Array.isArray(value)) {
          for (const item of value) {
            url.searchParams.append(key, item);
          }
        } else {
          url.searchParams.set(key, String(value));
        }
      }
    }

    const headers: Record<string, string> = {
      Accept: "application/json",
    };
    if (options.body !== undefined) {
      headers["Content-Type"] = "application/json";
    }

    const auth = options.auth ?? "user";
    if (auth === "apiKey") {
      if (!this.apiKey) {
        throw new TorchwoodError("API key is required for this request", 0);
      }
      headers["X-Api-Key"] = this.apiKey;
      headers["X-Torchwood-Project"] = this.projectId;
    } else if (auth === "user") {
      if (this.accessToken) {
        headers.Authorization = `Bearer ${this.accessToken}`;
      }
    }

    const res = await this.fetchImpl(url, {
      method,
      headers,
      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    });

    if (res.status === 204 || res.headers.get("content-length") === "0") {
      return undefined as T;
    }

    if (!res.ok) {
      throw await parseErrorResponse(res);
    }

    const text = await res.text();
    if (!text) {
      return undefined as T;
    }
    return JSON.parse(text) as T;
  }

  async requestForm<T>(
    method: string,
    path: string,
    form: FormData,
    auth: AuthMode = "apiKey"
  ): Promise<T> {
    const url = `${this.endpoint}${path.startsWith("/") ? path : `/${path}`}`;
    const headers: Record<string, string> = {};
    if (auth === "apiKey") {
      if (!this.apiKey) {
        throw new TorchwoodError("API key is required for this request", 0);
      }
      headers["X-Api-Key"] = this.apiKey;
      headers["X-Torchwood-Project"] = this.projectId;
    } else if (auth === "user" && this.accessToken) {
      headers.Authorization = `Bearer ${this.accessToken}`;
    }

    const res = await this.fetchImpl(url, { method, headers, body: form });
    if (!res.ok) {
      throw await parseErrorResponse(res);
    }
    return (await res.json()) as T;
  }
}

export function listQuery(params?: { queries?: string[]; page_size?: number; page_token?: string }) {
  if (!params) return undefined;
  return {
    queries: params.queries,
    page_size: params.page_size,
    page_token: params.page_token,
  };
}
