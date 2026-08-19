import { listQuery, type HttpTransport } from "../http.js";
import type { AssetDef, AssetHolding, AssetLedgerEntry, ListParams } from "../types.js";

export class ClientAssetsService {
  constructor(private readonly http: HttpTransport) {}

  async listAssetDefs(params?: ListParams): Promise<AssetDef[]> {
    const res = await this.http.request<{ defs: AssetDef[] }>("GET", "/v1/assets/defs", {
      query: listQuery(params),
    });
    return res.defs ?? [];
  }

  async listMyAssets(params?: ListParams): Promise<AssetHolding[]> {
    const res = await this.http.request<{ holdings: AssetHolding[] }>("GET", "/v1/assets", {
      query: listQuery(params),
    });
    return res.holdings ?? [];
  }

  async listMyAssetLedger(params?: ListParams & { def_code?: string }): Promise<AssetLedgerEntry[]> {
    const res = await this.http.request<{ entries: AssetLedgerEntry[] }>("GET", "/v1/assets/ledger", {
      query: {
        ...listQuery(params),
        def_code: params?.def_code,
      },
    });
    return res.entries ?? [];
  }
}
