import { listQuery, type HttpTransport } from "../http.js";
import type { ListParams, ManualFulfillResponse, PaymentOrder } from "../types.js";

export class ServerPaymentsService {
  constructor(private readonly http: HttpTransport) {}

  async listOrders(params?: ListParams): Promise<PaymentOrder[]> {
    const res = await this.http.request<{ orders: PaymentOrder[] }>(
      "GET",
      "/v1/server/payments/orders",
      { auth: "apiKey", query: listQuery(params) }
    );
    return res.orders ?? [];
  }

  async getOrder(orderId: string): Promise<PaymentOrder> {
    return this.http.request<PaymentOrder>("GET", `/v1/server/payments/orders/${orderId}`, {
      auth: "apiKey",
    });
  }

  async refund(
    orderId: string,
    input?: { amount?: string; reason?: string }
  ): Promise<PaymentOrder> {
    return this.http.request<PaymentOrder>(
      "POST",
      `/v1/server/payments/orders/${orderId}:refund`,
      { auth: "apiKey", body: { order_id: orderId, ...input } }
    );
  }

  async manualFulfill(orderId: string, reason?: string): Promise<ManualFulfillResponse> {
    return this.http.request<ManualFulfillResponse>(
      "POST",
      `/v1/server/payments/orders/${orderId}:fulfill`,
      { auth: "apiKey", body: { order_id: orderId, reason } }
    );
  }
}
