import { api } from "./client";
import type { ApiRequestConfig } from "./client";

export interface AssetDef {
  id: string;
  project_id?: string;
  code: string;
  name: string;
  class: string;
  decimals: number;
  max_quantity?: string;
  expires_in?: string;
  tradable?: boolean;
  unique_per_owner?: boolean;
  upgradeable?: boolean;
  metadata?: Record<string, unknown>;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface AssetHolding {
  id: string;
  owner_id?: string;
  def_id: string;
  def_code: string;
  class: string;
  quantity: string;
  expires_at?: string;
  level?: number;
}

export interface AssetLedgerEntry {
  id: string;
  owner_id?: string;
  def_id: string;
  def_code?: string;
  kind: string;
  delta: string;
  quantity_after: string;
  created_at?: string;
}

export async function listAssetDefs(): Promise<AssetDef[]> {
  const res = await api.get<{ defs: AssetDef[] }>("/server/assets/defs");
  return res.data.defs ?? [];
}

export async function getAssetDef(defId: string): Promise<AssetDef> {
  const res = await api.get<AssetDef>(`/server/assets/defs/${defId}`);
  return res.data;
}

export async function createAssetDef(input: {
  code: string;
  name: string;
  class: string;
  decimals?: number;
  max_quantity?: string;
  expires_in?: string;
  tradable?: boolean;
  unique_per_owner?: boolean;
  upgradeable?: boolean;
}): Promise<AssetDef> {
  const res = await api.post<AssetDef>("/server/assets/defs", input);
  return res.data;
}

export async function updateAssetDef(
  defId: string,
  input: { name?: string; status?: string; tradable?: boolean }
): Promise<AssetDef> {
  const res = await api.patch<AssetDef>(`/server/assets/defs/${defId}`, { def_id: defId, ...input });
  return res.data;
}

export async function deleteAssetDef(defId: string, config?: ApiRequestConfig): Promise<void> {
  await api.delete(`/server/assets/defs/${defId}`, config);
}

export async function listUserAssets(ownerId: string): Promise<AssetHolding[]> {
  const res = await api.get<{ holdings: AssetHolding[] }>(`/server/assets/users/${ownerId}`);
  return res.data.holdings ?? [];
}

export async function listUserLedger(ownerId: string): Promise<AssetLedgerEntry[]> {
  const res = await api.get<{ entries: AssetLedgerEntry[] }>(`/server/assets/users/${ownerId}/ledger`);
  return res.data.entries ?? [];
}
