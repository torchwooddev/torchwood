import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { vectorSearch, eq, orderDesc } from "../query.js";

describe("query builders", () => {
  it("vectorSearch 默认缺省 metric（COSINE 由服务端归一），链式字段落位", () => {
    const base = vectorSearch("emb", [0.1, 0.2, 0.3]).build();
    assert.deepEqual(base, { attribute: "emb", values: [0.1, 0.2, 0.3] });

    // 维度校验在服务端（按 catalog dims 拒绝）——构造器只透传字段。
    const full = vectorSearch("emb", [1, 0, 0])
      .metric("INNER_PRODUCT")
      .maxDistance(-0.2)
      .build();
    assert.equal(full.attribute, "emb");
    assert.deepEqual(full.values, [1, 0, 0]);
    assert.equal(full.metric, "INNER_PRODUCT");
    assert.equal(full.maxDistance, -0.2); // inner_product 阈值可为 0/负

    // B7：efSearch 缺省不出现（服务端维持 pgvector 缺省 40），设置后透传。
    assert.ok(!("efSearch" in base), "default builder must not emit efSearch");
    const tuned = vectorSearch("emb", [1, 0, 0]).efSearch(200).build();
    assert.equal(tuned.efSearch, 200);
  });

  it("既有构造器形态不变（回归）", () => {
    assert.deepEqual(eq("status", "open"), {
      eq: { attribute: "status", values: ["open"] },
    });
    assert.deepEqual(orderDesc("$createdAt"), {
      attribute: "$createdAt",
      desc: true,
    });
  });
});
