import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { accountsChannel } from "../client/realtime.js";
import { ClientPaymentsService } from "../client/payments.js";
import { ClientAssetsService } from "../client/assets.js";
import { ServerAssetsService } from "../server/assets.js";
import { HttpTransport } from "../http.js";

function captureFetch(): { calls: { method: string; url: URL; body?: Record<string, unknown> }[]; fetch: typeof fetch } {
  const calls: { method: string; url: URL; body?: Record<string, unknown> }[] = [];
  const fetchImpl: typeof fetch = async (input, init) => {
    calls.push({
      method: init?.method ?? "GET",
      url: new URL(String(input)),
      body: init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : undefined,
    });
    return new Response(
      JSON.stringify({
        order: { id: "o1", amount: "1999", currency: "USD" },
        orders: [],
        defs: [],
        holdings: [{ id: "h1", quantity: "100" }],
        entries: [{ id: "e1", delta: "100", quantity_after: "100" }],
      }),
      { status: 200, headers: { "content-type": "application/json" } }
    );
  };
  return { calls, fetch: fetchImpl };
}

describe("economy SDK", () => {
  it("accountsChannel 生成 D17 频道名", () => {
    assert.equal(accountsChannel("u1"), "accounts.u1");
  });

  it("CreateOrder 金额以字符串发送（禁止 number/float）", async () => {
    const cap = captureFetch();
    const http = new HttpTransport({
      endpoint: "http://localhost:9080",
      projectId: "default",
      accessToken: "jwt",
      fetch: cap.fetch,
    });
    const payments = new ClientPaymentsService(http);
    await payments.createOrder({
      idempotency_key: "idem-1",
      provider: "stripe",
      amount: "1999",
      currency: "USD",
      purpose_kind: "topup",
      purpose: { currency_code: "gold", amount: "100" },
    });
    assert.equal(cap.calls.length, 1);
    assert.equal(cap.calls[0].url.pathname, "/v1/payments/orders");
    assert.equal(cap.calls[0].body?.amount, "1999");
    assert.equal(typeof cap.calls[0].body?.amount, "string");
    const purpose = cap.calls[0].body?.purpose as Record<string, unknown>;
    assert.equal(purpose.amount, "100");
  });

  it("Client 资产只读路径不含写动词", () => {
    const proto = ClientAssetsService.prototype as unknown as Record<string, unknown>;
    assert.equal(typeof proto.listMyAssets, "function");
    assert.equal(typeof proto.listAssetDefs, "function");
    assert.equal(typeof proto.listMyAssetLedger, "function");
    assert.equal(proto.grant, undefined);
    assert.equal(proto.consume, undefined);
    assert.equal(proto.transfer, undefined);
  });

  it("Server Grant quantity 以字符串发送", async () => {
    const cap = captureFetch();
    const http = new HttpTransport({
      endpoint: "http://localhost:9080",
      projectId: "default",
      apiKey: "k",
      fetch: cap.fetch,
    });
    const assets = new ServerAssetsService(http);
    await assets.grant({
      owner_id: "u1",
      def_code: "gold",
      quantity: "100",
      idempotency_key: "g1",
    });
    assert.equal(cap.calls[0].url.pathname, "/v1/server/assets:grant");
    assert.equal(cap.calls[0].body?.quantity, "100");
    assert.equal(typeof cap.calls[0].body?.quantity, "string");
  });
});
