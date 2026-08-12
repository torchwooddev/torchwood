import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { parseErrorResponse, TorchwoodError } from "../errors.js";

describe("parseErrorResponse", () => {
  it("解析网关 error.message / error.code", async () => {
    const res = new Response(
      JSON.stringify({ error: { message: "user not found", code: "NOT_FOUND" } }),
      { status: 404, statusText: "Not Found" }
    );
    const err = await parseErrorResponse(res);
    assert.ok(err instanceof TorchwoodError);
    assert.equal(err.status, 404);
    assert.equal(err.code, "NOT_FOUND");
    assert.equal(err.message, "user not found");
  });

  it("非 JSON 错误体回退 statusText", async () => {
    const res = new Response("<html>oops</html>", {
      status: 500,
      statusText: "Internal Server Error",
    });
    const err = await parseErrorResponse(res);
    assert.equal(err.message, "Internal Server Error");
    assert.equal(err.code, undefined);
  });

  it("空错误体回退 statusText", async () => {
    const res = new Response("", { status: 400, statusText: "Bad Request" });
    const err = await parseErrorResponse(res);
    assert.equal(err.message, "Bad Request");
  });
});
