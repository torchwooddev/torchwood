// Console 试听面板的 /v1/realtime WebSocket 客户端（v2 设计 §4.2/§4.3）：
// 同源 cookie（TORCHWOOD_session_console）握手，hello 先绑 project_id，
// 不带 access_token；服务端 30s ping 回 pong。15m 会话到期服务端以
// token_expired 关连接（无连接内 refresh）：断线后先走 /v1/console/auth
// refresh 续期 cookie，再重新 hello + 重订频道（不补历史）；refresh 失败
// 则进入 disconnected 状态，由 UI 提示「已断开，点击重连」。

import { refreshSession } from "./auth";

export type ListenStatus = "connecting" | "connected" | "disconnected";

export interface ListenEvent {
  channel: string;
  payload: Record<string, unknown>;
  at: Date;
}

/** 可注入的 WebSocket 最小接口（浏览器原生 WebSocket 满足）。 */
export interface ListenWebSocket {
  onopen: (() => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code: number; reason: string }) => void) | null;
  onerror: (() => void) | null;
  send(data: string): void;
  close(code?: number, reason?: string): void;
}

export interface CollectionListenerOptions {
  projectId: string;
  channel: string;
  onEvent: (ev: ListenEvent) => void;
  onStatus: (status: ListenStatus) => void;
  /** 会话续期（默认走 /v1/console/auth refresh）；测试可注入。 */
  refresh?: () => Promise<void>;
  /** 测试注入用；默认使用全局 WebSocket。 */
  webSocketFactory?: (url: string) => ListenWebSocket;
}

export interface CollectionListener {
  /** 主动关闭：不再自动重连。 */
  close(): void;
  /** 手动重连（disconnected 状态下由 UI 触发）。 */
  reconnect(): void;
}

type ServerFrame = {
  type: string;
  id?: string;
  code?: string;
  message?: string;
  channel?: string;
  payload?: Record<string, unknown>;
};

function realtimeURL(): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/v1/realtime`;
}

// startCollectionListener 建立试听连接：hello 绑 project_id（cookie 鉴权），
// 订阅集合频道；断线先 refresh 会话再重连重订，refresh 失败则置
// disconnected 等待手动重连。
export function startCollectionListener(
  options: CollectionListenerOptions
): CollectionListener {
  const refresh = options.refresh ?? refreshSession;
  let ws: ListenWebSocket | null = null;
  let closedByUser = false;

  const setStatus = (s: ListenStatus) => options.onStatus(s);

  const open = () => {
    if (closedByUser) return;
    setStatus("connecting");
    const factory =
      options.webSocketFactory ??
      ((url: string) => new WebSocket(url) as unknown as ListenWebSocket);
    const conn = factory(realtimeURL());
    ws = conn;
    conn.onopen = () => {
      conn.send(JSON.stringify({ type: "hello", project_id: options.projectId }));
    };
    conn.onmessage = (ev) => {
      if (ws !== conn) return;
      let frame: ServerFrame;
      try {
        frame = JSON.parse(String(ev.data)) as ServerFrame;
      } catch {
        return;
      }
      switch (frame.type) {
        case "hello_ok":
          setStatus("connected");
          conn.send(
            JSON.stringify({ type: "subscribe", id: "c1", channel: options.channel })
          );
          break;
        case "event":
          if (frame.channel === options.channel) {
            options.onEvent({
              channel: frame.channel,
              payload: frame.payload ?? {},
              at: new Date(),
            });
          }
          break;
        case "ping":
          conn.send(JSON.stringify({ type: "pong" }));
          break;
        default:
          break;
      }
    };
    conn.onerror = () => {
      // onclose 随后触发，统一走断线逻辑。
    };
    conn.onclose = () => {
      if (closedByUser || ws !== conn) return;
      ws = null;
      void reconnectWithRefresh();
    };
  };

  // 断线恢复（§4.2）：先 refresh 会话 cookie 再重新 hello + 重订；
  // refresh 失败（会话彻底失效）→ disconnected，等 UI 手动重连。
  const reconnectWithRefresh = async () => {
    try {
      await refresh();
    } catch {
      if (!closedByUser) setStatus("disconnected");
      return;
    }
    open();
  };

  open();

  return {
    close() {
      closedByUser = true;
      const conn = ws;
      ws = null;
      conn?.close();
    },
    reconnect() {
      if (closedByUser) return;
      const conn = ws;
      ws = null;
      conn?.close();
      void reconnectWithRefresh();
    },
  };
}
