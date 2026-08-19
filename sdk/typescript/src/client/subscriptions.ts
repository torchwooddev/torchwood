import { listQuery, type HttpTransport } from "../http.js";
import type { ListParams, SubscribeResponse, Subscription, SubscriptionPlan } from "../types.js";

export class ClientSubscriptionsService {
  constructor(private readonly http: HttpTransport) {}

  async listPlans(params?: ListParams): Promise<SubscriptionPlan[]> {
    const res = await this.http.request<{ plans: SubscriptionPlan[] }>(
      "GET",
      "/v1/subscriptions/plans",
      { query: listQuery(params) }
    );
    return res.plans ?? [];
  }

  async subscribe(input: {
    plan_code: string;
    mode: string;
    idempotency_key: string;
    billing_asset_code?: string;
  }): Promise<SubscribeResponse> {
    return this.http.request<SubscribeResponse>("POST", "/v1/subscriptions", { body: input });
  }

  async getMySubscription(planCode?: string): Promise<Subscription> {
    return this.http.request<Subscription>("GET", "/v1/subscriptions/me", {
      query: { plan_code: planCode },
    });
  }

  async cancel(subscriptionId: string): Promise<Subscription> {
    return this.http.request<Subscription>(
      "POST",
      `/v1/subscriptions/${subscriptionId}:cancel`,
      { body: { subscription_id: subscriptionId } }
    );
  }
}
