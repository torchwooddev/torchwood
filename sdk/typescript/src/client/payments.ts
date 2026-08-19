import { listQuery, type HttpTransport } from "../http.js";
import type { CreateOrderResponse, ListParams, PaymentOrder } from "../types.js";

export class ClientPaymentsService {
  constructor(private readonly http: HttpTransport) {}

  async createOrder(input: {
    idempotency_key: string;
    provider: string;
    amount: string;
    currency: string;
    purpose_kind: string;
    purpose?: Record<string, unknown>;
  }): Promise<CreateOrderResponse> {
    return this.http.request<CreateOrderResponse>("POST", "/v1/payments/orders", {
      body: input,
    });
  }

  async getMyOrder(orderId: string): Promise<PaymentOrder> {
    return this.http.request<PaymentOrder>("GET", `/v1/payments/orders/${orderId}`);
  }

  async listMyOrders(params?: ListParams): Promise<PaymentOrder[]> {
    const res = await this.http.request<{ orders: PaymentOrder[] }>("GET", "/v1/payments/orders", {
      query: listQuery(params),
    });
    return res.orders ?? [];
  }

  async verifyReceipt(input: {
    order_id: string;
    receipt: string;
  }): Promise<{ order: PaymentOrder; transaction_id: string; idempotent_replay?: boolean }> {
    return this.http.request("POST", "/v1/payments/ios/verify-receipt", { body: input });
  }
}
