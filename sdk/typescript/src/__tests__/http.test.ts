import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { HttpTransport, listQuery } from "../http.js";

type FetchCall = { url: URL; headers: Record<string, string>; body: string };

function captureFetch(): { calls: FetchCall[]; fetch: typeof fetch } {
  const calls: FetchCall[] = [];
  const fetchImpl = async (input: RequestInfo | URL, init?: RequestInit) => {
    const headers: Record<string, string> = {};
    if (init?.headers) new Headers(init.headers).forEach((v, k) => (headers[k] = v));
    calls.push({ url: new URL(String(input)), headers, body: String(init?.body ?? "") });
    return new Response(JSON.stringify({ ok: true }), { status: 200 });
  };
  return { calls, fetch: fetchImpl };
}

describe("HttpTransport.query", () => {
  it("数组参数展开为重复 query 参数（query 展开）", async () => {
    const { calls, fetch } = captureFetch();
    const http = new HttpTransport({
      endpoint: "http://localhost:9080",
      projectId: "default",
      apiKey: "k",
      fetch,
    });
    await http.request("GET", "/v1/server/users", {
      auth: "apiKey",
      query: listQuery({ queries: ['equal("a","1")', 'orderDesc("b")'], page_size: 10 }),
    });
    assert.equal(calls.length, 1);
    assert.deepEqual(calls[0].url.searchParams.getAll("queries"), [
      'equal("a","1")',
      'orderDesc("b")',
    ]);
    assert.equal(calls[0].url.searchParams.get("page_size"), "10");
    assert.equal(calls[0].headers["x-api-key"], "k");
  });

  it("undefined 查询参数被跳过", async () => {
    const { calls, fetch } = captureFetch();
    const http = new HttpTransport({ endpoint: "http://localhost:9080", projectId: "p", fetch });
    await http.request("GET", "/v1/account/me", {
      query: { project_id: undefined as unknown as string },
    });
    assert.equal(calls[0].url.searchParams.has("project_id"), false);
  });

  it("请求体 JSON 序列化", async () => {
    const { calls, fetch } = captureFetch();
    const http = new HttpTransport({ endpoint: "http://localhost:9080", projectId: "p", fetch });
    await http.request("PATCH", "/v1/account", { body: { name: "New" } });
    assert.equal(calls[0].headers["content-type"], "application/json");
    assert.deepEqual(JSON.parse(calls[0].body), { name: "New" });
  });
});
