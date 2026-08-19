// Realtime 实现 /v1/realtime WebSocket 订阅（v2 设计 §4.2/§4.3）：
// hello 握手（Client SDK 必须携带 access_token；Console 同源 cookie 场景
// 可省略）、subscribe/unsubscribe 帧、服务端 30s ping 回 pong。
// JWT 到期服务端以 token_expired 关连接（无连接内 refresh）：断线后重新
// 调 getAccessToken 拿新 token → 重新 hello → 重订全部频道，不补历史；
// 重连采用指数退避。version 跳号 / truncated 由调用方自行 GetDocument 补读。

import type { HttpTransport } from "../http.js";

/** 实时事件（payload 为 Envelope.ClientPayload()，不含 acl）。 */
export interface RealtimeEvent {
  channel: string;
  payload: Record<string, unknown>;
}

export type RealtimeHandler = (event: RealtimeEvent) => void;

export type RealtimeStatus = "connecting" | "connected" | "reconnecting" | "closed";

/** 退订句柄。 */
export interface RealtimeSubscription {
  readonly channel: string;
  unsubscribe(): void;
}

/** 连接句柄。 */
export interface RealtimeConnection {
  readonly status: RealtimeStatus;
  subscribe(channel: string, handler: RealtimeHandler): RealtimeSubscription;
  /** 主动关闭：不再自动重连。 */
  close(): void;
}

/** 可注入的 WebSocket 最小接口（浏览器 / Node>=21 原生 WebSocket 均满足）。 */
export interface RealtimeWebSocket {
  onopen: (() => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code: number; reason: string }) => void) | null;
  onerror: (() => void) | null;
  send(data: string): void;
  close(code?: number, reason?: string): void;
}

export interface RealtimeConnectOptions {
  /** 默认取 client 配置的 projectId。 */
  projectId?: string;
  /**
   * 取 access token；每次（重）连都会调用，token_expired 断线后应在此返回
   * 刷新后的新 token。Console 同源 cookie 场景可省略 / 返回空串
   * （hello 不带 access_token）。
   */
  getAccessToken?: () => string | undefined | Promise<string | undefined>;
  /** 测试注入用；默认使用全局 WebSocket。 */
  webSocketFactory?: (url: string) => RealtimeWebSocket;
  /** 重连退避：起始延迟（默认 500ms）。 */
  reconnectInitialDelayMs?: number;
  /** 重连退避：延迟上限（默认 10s）。 */
  reconnectMaxDelayMs?: number;
  onStatusChange?: (status: RealtimeStatus) => void;
}

// 服务端帧类型（§4.3）。
type ServerFrame = {
  type: string;
  id?: string;
  code?: string;
  message?: string;
  connection_id?: string;
  channel?: string;
  payload?: Record<string, unknown>;
};

class RealtimeConnectionImpl implements RealtimeConnection {
  private ws: RealtimeWebSocket | null = null;
  private readonly subs = new Map<string, Set<RealtimeHandler>>();
  private readonly subIds = new Map<string, string>();
  private subSeq = 0;
  private attempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private closedByUser = false;
  private _status: RealtimeStatus = "connecting";

  constructor(
    private readonly http: HttpTransport,
    private readonly options: RealtimeConnectOptions,
  ) {
    void this.connectOnce();
  }

  get status(): RealtimeStatus {
    return this._status;
  }

  private setStatus(s: RealtimeStatus): void {
    this._status = s;
    this.options.onStatusChange?.(s);
  }

  private wsURL(): string {
    const endpoint = this.http.getEndpoint();
    const url = new URL(endpoint);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    url.pathname = "/v1/realtime";
    url.search = "";
    return url.toString();
  }

  private async connectOnce(): Promise<void> {
    if (this.closedByUser) return;
    const token = await this.resolveAccessToken();
    let ws: RealtimeWebSocket;
    try {
      const factory =
        this.options.webSocketFactory ??
        ((url: string) => new WebSocket(url) as unknown as RealtimeWebSocket);
      ws = factory(this.wsURL());
    } catch {
      this.scheduleReconnect();
      return;
    }
    this.ws = ws;
    ws.onopen = () => {
      const hello: Record<string, string> = {
        type: "hello",
        project_id: this.options.projectId ?? this.http.getProjectId(),
      };
      if (token) hello.access_token = token;
      ws.send(JSON.stringify(hello));
    };
    ws.onmessage = (ev) => this.handleMessage(ws, ev.data);
    ws.onerror = () => {
      // onclose 随后触发，统一走重连逻辑。
    };
    ws.onclose = () => {
      if (this.closedByUser) {
        this.setStatus("closed");
        return;
      }
      this.scheduleReconnect();
    };
  }

  private async resolveAccessToken(): Promise<string | undefined> {
    if (this.options.getAccessToken) {
      const t = await this.options.getAccessToken();
      return t || undefined;
    }
    return this.http.getAccessToken();
  }

  private handleMessage(ws: RealtimeWebSocket, data: unknown): void {
    if (ws !== this.ws) return;
    let frame: ServerFrame;
    try {
      frame = JSON.parse(String(data)) as ServerFrame;
    } catch {
      return;
    }
    switch (frame.type) {
      case "hello_ok": {
        this.attempt = 0;
        this.setStatus("connected");
        // （重）连成功后重订全部频道，不补历史（§4.2）。
        for (const channel of this.subs.keys()) {
          this.sendSubscribe(channel);
        }
        break;
      }
      case "subscribed":
        break;
      case "event": {
        if (!frame.channel) break;
        const handlers = this.subs.get(frame.channel);
        if (!handlers) break;
        const event: RealtimeEvent = {
          channel: frame.channel,
          payload: frame.payload ?? {},
        };
        for (const h of handlers) h(event);
        break;
      }
      case "ping":
        ws.send(JSON.stringify({ type: "pong" }));
        break;
      case "pong":
        break;
      case "error":
        // 握手错误服务端随后会关连接，走 onclose 重连（重连时会重新取
        // token）；订阅级错误（NOT_FOUND / RESOURCE_EXHAUSTED）连接保持，
        // 由调用方决定是否退订。
        break;
    }
  }

  private scheduleReconnect(): void {
    if (this.closedByUser) return;
    this.setStatus("reconnecting");
    const initial = this.options.reconnectInitialDelayMs ?? 500;
    const max = this.options.reconnectMaxDelayMs ?? 10_000;
    const delay = Math.min(initial * 2 ** this.attempt, max);
    this.attempt += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.setStatus("connecting");
      void this.connectOnce();
    }, delay);
  }

  private sendSubscribe(channel: string): void {
    if (!this.ws || this._status !== "connected") return;
    this.ws.send(JSON.stringify({ type: "subscribe", id: this.subId(channel), channel }));
  }

  private subId(channel: string): string {
    let id = this.subIds.get(channel);
    if (!id) {
      id = `c${++this.subSeq}`;
      this.subIds.set(channel, id);
    }
    return id;
  }

  subscribe(channel: string, handler: RealtimeHandler): RealtimeSubscription {
    let handlers = this.subs.get(channel);
    if (!handlers) {
      handlers = new Set();
      this.subs.set(channel, handlers);
    }
    handlers.add(handler);
    if (handlers.size === 1) {
      this.sendSubscribe(channel);
    }
    return {
      channel,
      unsubscribe: () => {
        const set = this.subs.get(channel);
        if (!set) return;
        set.delete(handler);
        if (set.size > 0) return;
        this.subs.delete(channel);
        if (this.ws && this._status === "connected") {
          const id = this.subIds.get(channel);
          this.subIds.delete(channel);
          this.ws.send(JSON.stringify({ type: "unsubscribe", id, channel }));
        } else {
          this.subIds.delete(channel);
        }
      },
    };
  }

  close(): void {
    if (this.closedByUser) return;
    this.closedByUser = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    this.setStatus("closed");
  }
}

/** Realtime 服务入口：`client.realtime.connect({ projectId, getAccessToken })`。 */
export class RealtimeService {
  constructor(private readonly http: HttpTransport) {}

  connect(options: RealtimeConnectOptions = {}): RealtimeConnection {
    return new RealtimeConnectionImpl(this.http, options);
  }
}
