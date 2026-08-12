import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { HttpTransport } from "../http.js";
import { UsersService } from "../server/users.js";

describe("UsersService.labels 编码", () => {
  it("labels 直接透传对象，不做 {values:[...]} 包装", async () => {
    let sent: unknown;
    const fetchImpl = async (_input: RequestInfo | URL, init?: RequestInit) => {
      sent = JSON.parse(String(init?.body));
      return new Response(JSON.stringify({ id: "u1" }), { status: 200 });
    };
    const http = new HttpTransport({
      endpoint: "http://localhost:9080",
      projectId: "default",
      apiKey: "k",
      fetch: fetchImpl,
    });
    const users = new UsersService(http);

    await users.create({
      email: "a@example.com",
      password: "pw",
      labels: { region: "cn", tier: 2 },
    });
    assert.deepEqual(sent, {
      email: "a@example.com",
      password: "pw",
      labels: { region: "cn", tier: 2 },
    });

    await users.update("u1", { labels: { region: "eu" } });
    assert.deepEqual(sent, { labels: { region: "eu" } });
  });
});
