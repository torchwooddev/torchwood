import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

import { AccountService } from "../client/account.js";
import { ClientDatabasesService } from "../client/databases.js";
import { ClientTeamsService } from "../client/teams.js";
import {
  APIKeysService,
  FunctionsService,
  HealthService,
  OAuthProvidersService,
  ProjectsService,
  ServerDatabasesService,
  ServerTeamsService,
  StorageService,
  UsersService,
} from "../server/index.js";

// ---- 契约测试：proto（swagger.json 产物）↔ TS SDK 方法集合比对（F11-2）----
// 前提：仓库根目录执行过 `task generate-proto`（genproto/**/*.swagger.json 存在）。
// 任一 proto 新增 RPC 后，本测试失败并提示在 SDK 补齐对应方法。

const GENPROTO_DIR = join(dirname(fileURLToPath(import.meta.url)), "../../../../genproto");

// 服务 → TS SDK 类（swagger 的 operationId 格式为 "{Service}_{RPC}"）。
// 仅用于读取 prototype 方法，构造参数类型不参与检查。
interface ClassLike {
  prototype: object;
}
const SDK_SERVICES: Record<string, ClassLike> = {
  // Server API
  DatabasesService: ServerDatabasesService,
  FunctionsService: FunctionsService,
  StorageService: StorageService,
  TeamsService: ServerTeamsService,
  UsersService: UsersService,
  APIKeysService: APIKeysService,
  OAuthProvidersService: OAuthProvidersService,
  ProjectsService: ProjectsService,
  HealthService: HealthService,
  // Client API（swagger 服务名与 Server API 重名，加 client. 前缀区分）
  "client.AccountService": AccountService,
  "client.DatabasesService": ClientDatabasesService,
  "client.TeamsService": ClientTeamsService,
};

// RPC 名 → TS SDK 方法名（SDK 使用简短命名，与 proto 非一一对应，显式登记）。
const RPC_TO_METHOD: Record<string, Record<string, string>> = {
  DatabasesService: {
    CreateDatabase: "createDatabase",
    ListDatabases: "listDatabases",
    GetDatabase: "getDatabase",
    DeleteDatabase: "deleteDatabase",
    CreateCollection: "createCollection",
    ListCollections: "listCollections",
    GetCollection: "getCollection",
    DeleteCollection: "deleteCollection",
    UpdateCollection: "updateCollection",
    CreateAttribute: "createAttribute",
    DeleteAttribute: "deleteAttribute",
    CreateIndex: "createIndex",
    DeleteIndex: "deleteIndex",
    CreateDocument: "createDocument",
    ListDocuments: "listDocuments",
    GetDocument: "getDocument",
    UpdateDocument: "updateDocument",
    UpsertDocument: "upsertDocument",
    DeleteDocument: "deleteDocument",
    CountDocuments: "countDocuments",
    BulkUpdateDocuments: "bulkUpdateDocuments",
    BulkDeleteDocuments: "bulkDeleteDocuments",
  },
  FunctionsService: {
    ListRuntimes: "listRuntimes",
    ListSpecifications: "listSpecifications",
    CreateFunction: "create",
    ListFunctions: "list",
    GetFunction: "get",
    UpdateFunction: "update",
    DeleteFunction: "delete",
    CreateDeployment: "createDeployment",
    ListDeployments: "listDeployments",
    GetDeployment: "getDeployment",
    DeleteDeployment: "deleteDeployment",
    SetVariables: "setVariables",
    GetVariables: "getVariables",
    CreateExecution: "createExecution",
    ListExecutions: "listExecutions",
    GetExecution: "getExecution",
  },
  StorageService: {
    CreateBucket: "createBucket",
    ListBuckets: "listBuckets",
    GetBucket: "getBucket",
    DeleteBucket: "deleteBucket",
    UpdateBucket: "updateBucket",
    CreateFile: "uploadFile",
    ListFiles: "listFiles",
    GetFile: "getFile",
    DeleteFile: "deleteFile",
    UpdateFile: "updateFile",
    CreateFileToken: "createFileToken",
    GetStorageUsage: "getStorageUsage",
  },
  TeamsService: {
    CreateTeam: "create",
    ListTeams: "list",
    GetTeam: "get",
    DeleteTeam: "delete",
    GetTeamPrefs: "getPrefs",
    UpdateTeamPrefs: "updatePrefs",
    CreateMembership: "createMembership",
    ListMemberships: "listMemberships",
    GetMembership: "getMembership",
    UpdateMembership: "updateMembership",
    UpdateMembershipStatus: "updateMembershipStatus",
    DeleteMembership: "deleteMembership",
  },
  UsersService: {
    CreateUser: "create",
    ListUsers: "list",
    GetUser: "get",
    UpdateUser: "update",
    UpdateUserPassword: "updatePassword",
    DeleteUser: "delete",
    ListUserSessions: "listSessions",
    DeleteUserSession: "deleteSession",
    CreateUserToken: "createToken",
  },
  APIKeysService: {
    CreateAPIKey: "create",
    ListAPIKeys: "list",
    GetAPIKey: "get",
    DeleteAPIKey: "delete",
  },
  OAuthProvidersService: {
    ListOAuthProviders: "list",
    UpsertOAuthProvider: "upsert",
    DeleteOAuthProvider: "delete",
  },
  ProjectsService: {
    CreateProject: "create",
    ListProjects: "list",
    GetProject: "get",
    UpdateProject: "update",
  },
  HealthService: {
    Check: "check",
    Check2: "check", // additional_bindings 生成 operationId 后缀（/v1/server/health）
    GetVersion: "getVersion",
  },
  "client.AccountService": {
    SignUp: "signUp",
    SignIn: "signIn",
    SignOut: "signOut",
    RefreshToken: "refresh",
    Me: "me",
    UpdateAccount: "updateAccount",
    ListSessions: "listSessions",
    DeleteSession: "deleteSession",
    DeleteSessions: "deleteSessions",
    GetPrefs: "getPrefs",
    UpdatePrefs: "updatePrefs",
    CreateEmailOTP: "createEmailOTP",
    CreateEmailOTPSession: "createEmailOTPSession",
    CreateOAuth2Session: "createOAuth2Session",
    CreateOAuth2TokenSession: "createOAuth2TokenSession",
    CreatePhoneOTP: "createPhoneOTP",
    CreatePhoneOTPSession: "createPhoneOTPSession",
    CreateWeChatMiniProgramSession: "createWeChatMiniProgramSession",
    CreateAnonymousSession: "createAnonymousSession",
    CreateOAuth2LinkSession: "createOAuth2LinkSession",
    CreateOAuth2LinkTokenSession: "createOAuth2LinkTokenSession",
    CreateVerification: "createVerification",
    UpdateVerification: "updateVerification",
    CreateRecovery: "createRecovery",
    UpdateRecovery: "updateRecovery",
    ListFactors: "listFactors",
    CreateTOTPFactor: "createTOTPFactor",
    VerifyTOTPFactor: "verifyTOTPFactor",
    DeleteFactor: "deleteFactor",
    CreateMFASession: "createMFASession",
    CreateJWT: "createJWT",
    CreateMagicURLSession: "createMagicURLSession",
    UpdateMagicURLSession: "updateMagicURLSession",
    ListLogs: "listLogs",
  },
  // Client API（swagger 文件内服务名为 DatabasesService / TeamsService，
  // 与 Server API 重名，SDK 类为 Client* 前缀，见 loadServiceFiles 重映射）。
  "client.DatabasesService": {
    CreateDocument: "createDocument",
    ListDocuments: "listDocuments",
    GetDocument: "getDocument",
    UpdateDocument: "updateDocument",
    UpsertDocument: "upsertDocument",
    DeleteDocument: "deleteDocument",
    CountDocuments: "countDocuments",
  },
  "client.TeamsService": {
    CreateTeam: "createTeam",
    ListTeams: "listTeams",
    GetTeam: "getTeam",
    DeleteTeam: "deleteTeam",
    CreateMembership: "createMembership",
    ListMemberships: "listMemberships",
    UpdateMembershipStatus: "updateMembershipStatus",
    DeleteMembership: "deleteMembership",
  },
};

// securityDefinitions 中必须存在的三个统一 scheme 名（F11-1）。
const REQUIRED_SCHEMES = ["apiKey", "Bearer", "cookie"];

// 服务默认 access（swagger 顶层 x-torchwood-access）→ 期望的全局 security。
const EXPECTED_GLOBAL_SECURITY: Record<string, string[]> = {
  api_key: ["apiKey"],
  authenticated: ["Bearer"],
  permission: ["cookie"],
  public: [],
};

interface SwaggerFile {
  name: string;
  doc: {
    securityDefinitions?: Record<string, unknown>;
    security?: Array<Record<string, unknown>>;
    "x-torchwood-access"?: string;
    paths?: Record<string, Record<string, { operationId?: string; security?: unknown[]; "x-torchwood-access"?: string }>>;
  };
}

function loadSwaggerFiles(): SwaggerFile[] {
  const files: SwaggerFile[] = [];
  for (const sub of ["server", "client", "console", "shared"]) {
    const dir = join(GENPROTO_DIR, sub, "v1");
    if (!existsSync(dir)) continue;
    for (const f of readdirSync(dir).filter((n) => n.endsWith(".swagger.json"))) {
      files.push({ name: `${sub}/${f}`, doc: JSON.parse(readFileSync(join(dir, f), "utf8")) });
    }
  }
  return files;
}

describe("contract: swagger ↔ TS SDK", () => {
  const files = loadSwaggerFiles();
  assert.ok(files.length >= 14, `genproto swagger 文件缺失（当前 ${files.length} 个），请先执行 task generate-proto`);

  it("所有服务 swagger 声明统一 securityDefinitions", () => {
    for (const f of files) {
      // shared 包为纯消息定义（无 HTTP 绑定），不生成 security。
      if (f.name.startsWith("shared/")) continue;
      const defs = f.doc.securityDefinitions ?? {};
      for (const scheme of REQUIRED_SCHEMES) {
        assert.ok(defs[scheme], `${f.name} 缺少 securityDefinitions.${scheme}`);
      }
    }
  });

  it("顶层 x-torchwood-access 与全局 security 一致", () => {
    for (const f of files) {
      if (f.name.startsWith("shared/")) continue;
      const access = f.doc["x-torchwood-access"];
      assert.ok(access, `${f.name} 缺少顶层 x-torchwood-access`);
      const expected = EXPECTED_GLOBAL_SECURITY[access!];
      assert.ok(expected, `${f.name} 的 x-torchwood-access=${access} 非法`);
      const sec = (f.doc.security ?? []).map((r) => Object.keys(r)[0]);
      assert.deepEqual(sec.sort(), [...expected].sort(), `${f.name} 全局 security 与 ${access} 不符`);
    }
  });

  it("ACCESS_PUBLIC 方法必须声明 security: []（匿名）", () => {
    for (const f of files) {
      for (const path of Object.values(f.doc.paths ?? {})) {
        for (const op of Object.values(path)) {
          if (op?.["x-torchwood-access"] === "public") {
            assert.deepEqual(op.security ?? [], [], `${f.name} ${op.operationId} 公开方法必须 security: []`);
          }
        }
      }
    }
  });

  it("proto RPC 集合 ⊆ TS SDK 方法集合", () => {
    let rpcCount = 0;
    for (const f of files) {
      if (!f.name.startsWith("server/") && !f.name.startsWith("client/")) continue;
      const isClient = f.name.startsWith("client/");
      for (const path of Object.values(f.doc.paths ?? {})) {
        for (const op of Object.values(path)) {
          const operationId = op?.operationId;
          if (!operationId) continue;
          const [service, rpc] = operationId.split("_");
          const key = isClient ? `client.${service}` : service;
          const cls = SDK_SERVICES[key];
          assert.ok(cls, `${f.name} 未登记服务 ${service} 的 SDK 映射`);
          const method = RPC_TO_METHOD[key]?.[rpc];
          assert.ok(method, `${f.name} ${operationId} 未登记 RPC→方法映射`);
          const proto = cls.prototype as unknown as Record<string, unknown>;
          assert.equal(
            typeof proto[method],
            "function",
            `${operationId} 应映射到 ${key}.${method}()`
          );
          rpcCount++;
        }
      }
    }
    // 服务器 80 + 客户端 34 的基线，防误删。
    assert.ok(rpcCount >= 110, `RPC 总数异常（当前 ${rpcCount}），请核对 genproto 产物`);
  });
});
