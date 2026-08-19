import { api } from "./client";
import type { ApiRequestConfig } from "./client";

export interface PaymentOrder {
  id: string;
  project_id?: string;
  user_id?: string;
  provider: string;
  amount: string;
  currency: string;
  purpose_kind: string;
  purpose?: Record<string, unknown>;
  status: string;
  idempotency_key?: string;
  provider_session_id?: string;
  provider_order_id?: string;
  created_at?: string;
  paid_at?: string;
  expires_at?: string;
}

export async function listOrders(): Promise<PaymentOrder[]> {
  const res = await api.get<{ orders: PaymentOrder[] }>("/server/payments/orders");
  return res.data.orders ?? [];
}

export async function getOrder(orderId: string): Promise<PaymentOrder> {
  const res = await api.get<PaymentOrder>(`/server/payments/orders/${orderId}`);
  return res.data;
}

export async function refundOrder(
  orderId: string,
  input?: { amount?: string; reason?: string },
  config?: ApiRequestConfig
): Promise<PaymentOrder> {
  const res = await api.post<PaymentOrder>(
    `/server/payments/orders/${orderId}:refund`,
    { order_id: orderId, ...input },
    config
  );
  return res.data;
}

export async function manualFulfillOrder(
  orderId: string,
  reason?: string,
  config?: ApiRequestConfig
): Promise<{ order: PaymentOrder }> {
  const res = await api.post<{ order: PaymentOrder }>(
    `/server/payments/orders/${orderId}:fulfill`,
    { order_id: orderId, reason },
    config
  );
  return res.data;
}
