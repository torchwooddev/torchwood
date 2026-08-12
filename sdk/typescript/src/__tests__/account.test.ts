import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { HttpTransport } from "../http.js";
import { AccountService } from "../client/account.js";

function stubFetch(body: unknown, status = 200): typeof fetch {
  return async () =>
    new Response(JSON.stringify(body), {
      status,
      headers: { "content-type": "application/json" },
    });
}

describe("AccountService MFA 分支", () => {
  it("signIn mfa_required 时返回 challenge_token 且不保存 token", async () => {
    const http = new HttpTransport({
      endpoint: "http://localhost:9080",
      projectId: "default",
      fetch: stubFetch({
        mfa_required: true,
        challenge_token: "ch-1",
        factors: [{ id: "f1", type: "totp", status: "verified" }],
      }),
    });
    const account = new AccountService(http);

    const res = await account.signIn({ email: "u@example.com", password: "pw" });
    assert.equal(res.mfa_required, true);
    assert.equal(res.challenge_token, "ch-1");
    assert.equal(res.factors?.[0]?.type, "totp");
    assert.equal(http.getAccessToken(), undefined);
  });

  it("signUp 成功分支保存 access token", async () => {
    const http = new HttpTransport({
      endpoint: "http://localhost:9080",
      projectId: "default",
      fetch: stubFetch({
        account: { id: "u1" },
        tokens: { access_token: "at-1", refresh_token: "rt-1", expires_at: "2026-08-13T06:00:00Z" },
      }),
    });
    const account = new AccountService(http);

    const res = await account.signUp({ email: "u@example.com", password: "pw", name: "User" });
    assert.equal(http.getAccessToken(), "at-1");
    assert.equal(res.tokens?.access_token, "at-1");
    assert.equal(res.tokens?.expires_at, "2026-08-13T06:00:00Z");
  });
});

describe("AccountService.deleteSessions", () => {
  it("keep_current 作为查询参数传递", async () => {
    const calls: string[] = [];
    const fetchImpl = async (input: RequestInfo | URL) => {
      calls.push(String(input));
      return new Response(null, { status: 204 });
    };
    const http = new HttpTransport({
      endpoint: "http://localhost:9080",
      projectId: "default",
      fetch: fetchImpl,
    });
    const account = new AccountService(http);

    await account.deleteSessions(true);
    assert.equal(calls.length, 1);
    const url = new URL(calls[0]);
    assert.equal(url.pathname, "/v1/account/sessions");
    assert.equal(url.searchParams.get("keep_current"), "true");
  });
});

describe("AccountService.deleteFactor", () => {
  it("携带 code 时经 query 传递；未携带时无 query", async () => {
    const calls: string[] = [];
    const fetchImpl = async (input: RequestInfo | URL) => {
      calls.push(String(input));
      return new Response(null, { status: 204 });
    };
    const http = new HttpTransport({
      endpoint: "http://localhost:9080",
      projectId: "default",
      fetch: fetchImpl,
    });
    const account = new AccountService(http);

    await account.deleteFactor("f1", "123456");
    assert.equal(calls.length, 1);
    let url = new URL(calls[0]);
    assert.equal(url.pathname, "/v1/account/mfa/f1");
    assert.equal(url.searchParams.get("code"), "123456");

    await account.deleteFactor("f2");
    assert.equal(calls.length, 2);
    url = new URL(calls[1]);
    assert.equal(url.pathname, "/v1/account/mfa/f2");
    assert.equal(url.searchParams.has("code"), false);
  });
});
