import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  TOOL_CREATE_DOCUMENT,
  TOOL_CREATE_USER,
  TOOL_DELETE_DOCUMENT,
  TOOL_GET_COLLECTION,
  TOOL_GET_DOCUMENT,
  TOOL_GET_FILE,
  TOOL_GET_HEALTH,
  TOOL_GET_ORDER,
  TOOL_GET_USER,
  TOOL_GRANT_ASSET,
  TOOL_INVOKE_FUNCTION,
  TOOL_LIST_COLLECTIONS,
  TOOL_LIST_FILES,
  TOOL_LIST_USER_ASSETS,
  TOOL_LIST_USERS,
  TOOL_QUERY_DOCUMENTS,
  TOOL_UPDATE_DOCUMENT,
  TOOL_UPSERT_DOCUMENT,
  agentTools,
  lookupAgentTool,
} from "../server/tools.js";

describe("agentTools catalog", () => {
  it("maps 18 names to known FullMethod strings and excludes API key methods", () => {
    const want: Array<{ name: string; fullMethod: string }> = [
      { name: TOOL_LIST_USERS, fullMethod: "/torchwood.server.v1.UsersService/ListUsers" },
      { name: TOOL_GET_USER, fullMethod: "/torchwood.server.v1.UsersService/GetUser" },
      { name: TOOL_CREATE_USER, fullMethod: "/torchwood.server.v1.UsersService/CreateUser" },
      {
        name: TOOL_QUERY_DOCUMENTS,
        fullMethod: "/torchwood.server.v1.DatabasesService/ListDocuments",
      },
      { name: TOOL_GET_DOCUMENT, fullMethod: "/torchwood.server.v1.DatabasesService/GetDocument" },
      {
        name: TOOL_CREATE_DOCUMENT,
        fullMethod: "/torchwood.server.v1.DatabasesService/CreateDocument",
      },
      {
        name: TOOL_UPDATE_DOCUMENT,
        fullMethod: "/torchwood.server.v1.DatabasesService/UpdateDocument",
      },
      {
        name: TOOL_UPSERT_DOCUMENT,
        fullMethod: "/torchwood.server.v1.DatabasesService/UpsertDocument",
      },
      {
        name: TOOL_DELETE_DOCUMENT,
        fullMethod: "/torchwood.server.v1.DatabasesService/DeleteDocument",
      },
      {
        name: TOOL_LIST_COLLECTIONS,
        fullMethod: "/torchwood.server.v1.DatabasesService/ListCollections",
      },
      {
        name: TOOL_GET_COLLECTION,
        fullMethod: "/torchwood.server.v1.DatabasesService/GetCollection",
      },
      {
        name: TOOL_INVOKE_FUNCTION,
        fullMethod: "/torchwood.server.v1.FunctionsService/CreateExecution",
      },
      { name: TOOL_LIST_FILES, fullMethod: "/torchwood.server.v1.StorageService/ListFiles" },
      { name: TOOL_GET_FILE, fullMethod: "/torchwood.server.v1.StorageService/GetFile" },
      { name: TOOL_GRANT_ASSET, fullMethod: "/torchwood.server.v1.AssetsService/Grant" },
      {
        name: TOOL_LIST_USER_ASSETS,
        fullMethod: "/torchwood.server.v1.AssetsService/ListUserAssets",
      },
      { name: TOOL_GET_ORDER, fullMethod: "/torchwood.server.v1.PaymentsService/GetOrder" },
      { name: TOOL_GET_HEALTH, fullMethod: "/torchwood.server.v1.HealthService/Check" },
    ];

    assert.equal(want.length, 18);
    assert.equal(agentTools.length, 18);

    const seen = new Set<string>();
    for (const [i, tool] of agentTools.entries()) {
      assert.equal(tool.name, want[i].name);
      assert.equal(tool.fullMethod, want[i].fullMethod);
      assert.equal(tool.fullMethod.includes("APIKeys"), false);
      assert.deepEqual(lookupAgentTool(tool.name), tool);
      assert.equal(seen.has(tool.name), false);
      seen.add(tool.name);
    }

    for (const method of [
      "/torchwood.server.v1.APIKeysService/CreateAPIKey",
      "/torchwood.server.v1.APIKeysService/ListAPIKeys",
      "/torchwood.server.v1.APIKeysService/GetAPIKey",
      "/torchwood.server.v1.APIKeysService/DeleteAPIKey",
    ]) {
      assert.equal(
        agentTools.some((t) => t.fullMethod === method),
        false
      );
    }
    for (const name of ["create_api_key", "list_api_keys", "get_api_key", "delete_api_key"]) {
      assert.equal(lookupAgentTool(name), undefined);
    }
  });
});
