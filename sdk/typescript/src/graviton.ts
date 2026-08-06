import { AccountService, ClientDatabasesService, ClientTeamsService } from "./client/index.js";
import type { TorchwoodConfig } from "./http.js";
import { HttpTransport } from "./http.js";
import {
  APIKeysService,
  HealthService,
  OAuthProvidersService,
  ProjectsService,
  ServerDatabasesService,
  ServerTeamsService,
  StorageService,
  UsersService,
} from "./server/index.js";

export type { TorchwoodConfig } from "./http.js";
export { TorchwoodError } from "./errors.js";
export * from "./types.js";

export class Torchwood {
  readonly account: AccountService;
  readonly databases: ClientDatabasesService;
  readonly teams: ClientTeamsService;

  readonly server: {
    health: HealthService;
    projects: ProjectsService;
    users: UsersService;
    teams: ServerTeamsService;
    databases: ServerDatabasesService;
    apiKeys: APIKeysService;
    oauthProviders: OAuthProvidersService;
    storage: StorageService;
  };

  private readonly transport: HttpTransport;

  constructor(config: TorchwoodConfig) {
    this.transport = new HttpTransport(config);
    this.account = new AccountService(this.transport);
    this.databases = new ClientDatabasesService(this.transport);
    this.teams = new ClientTeamsService(this.transport);
    this.server = {
      health: new HealthService(this.transport),
      projects: new ProjectsService(this.transport),
      users: new UsersService(this.transport),
      teams: new ServerTeamsService(this.transport),
      databases: new ServerDatabasesService(this.transport),
      apiKeys: new APIKeysService(this.transport),
      oauthProviders: new OAuthProvidersService(this.transport),
      storage: new StorageService(this.transport),
    };
  }

  static create(config: TorchwoodConfig): Torchwood {
    return new Torchwood(config);
  }

  /** Server API + optional Client API with a project API key. */
  static withApiKey(endpoint: string, projectId: string, apiKey: string): Torchwood {
    return new Torchwood({ endpoint, projectId, apiKey });
  }

  /** Client API with an existing user access token. */
  static withAccessToken(endpoint: string, projectId: string, accessToken: string): Torchwood {
    return new Torchwood({ endpoint, projectId, accessToken });
  }

  setAccessToken(token: string | undefined): void {
    this.transport.setAccessToken(token);
  }

  getAccessToken(): string | undefined {
    return this.transport.getAccessToken();
  }

  getProjectId(): string {
    return this.transport.getProjectId();
  }
}
