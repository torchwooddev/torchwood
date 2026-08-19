import { listQuery, type HttpTransport } from "../http.js";
import type { Benefits, ListParams, ProviderOverrides, Subscription, SubscriptionPlan } from "../types.js";

export class ServerSubscriptionsService {
  constructor(private readonly http: HttpTransport) {}

  async createPlan(input: {
    code: string;
    name: string;
    amount: string;
    currency: string;
    interval: string;
    interval_days?: string;
    grace_days?: number;
    trial_days?: number;
    benefits?: Benefits;
    provider_overrides?: ProviderOverrides;
  }): Promise<SubscriptionPlan> {
    return this.http.request<SubscriptionPlan>("POST", "/v1/server/subscriptions/plans", {
      auth: "apiKey",
      body: input,
    });
  }

  async listPlans(params?: ListParams): Promise<SubscriptionPlan[]> {
    const res = await this.http.request<{ plans: SubscriptionPlan[] }>(
      "GET",
      "/v1/server/subscriptions/plans",
      { auth: "apiKey", query: listQuery(params) }
    );
    return res.plans ?? [];
  }

  async getPlan(planId: string): Promise<SubscriptionPlan> {
    return this.http.request<SubscriptionPlan>("GET", `/v1/server/subscriptions/plans/${planId}`, {
      auth: "apiKey",
    });
  }

  async updatePlan(
    planId: string,
    input: {
      name?: string;
      amount?: string;
      currency?: string;
      interval?: string;
      interval_days?: string;
      grace_days?: number;
      trial_days?: number;
      benefits?: Benefits;
      provider_overrides?: ProviderOverrides;
      status?: string;
    }
  ): Promise<SubscriptionPlan> {
    return this.http.request<SubscriptionPlan>("PATCH", `/v1/server/subscriptions/plans/${planId}`, {
      auth: "apiKey",
      body: { plan_id: planId, ...input },
    });
  }

  async deletePlan(planId: string): Promise<void> {
    await this.http.request<void>("DELETE", `/v1/server/subscriptions/plans/${planId}`, {
      auth: "apiKey",
    });
  }

  async listSubscriptions(params?: ListParams): Promise<Subscription[]> {
    const res = await this.http.request<{ subscriptions: Subscription[] }>(
      "GET",
      "/v1/server/subscriptions",
      { auth: "apiKey", query: listQuery(params) }
    );
    return res.subscriptions ?? [];
  }

  async getSubscription(subscriptionId: string): Promise<Subscription> {
    return this.http.request<Subscription>("GET", `/v1/server/subscriptions/${subscriptionId}`, {
      auth: "apiKey",
    });
  }

  async cancelSubscription(subscriptionId: string, reason?: string): Promise<Subscription> {
    return this.http.request<Subscription>(
      "POST",
      `/v1/server/subscriptions/${subscriptionId}:cancel`,
      { auth: "apiKey", body: { subscription_id: subscriptionId, reason } }
    );
  }

  async expireSubscription(subscriptionId: string, reason?: string): Promise<Subscription> {
    return this.http.request<Subscription>(
      "POST",
      `/v1/server/subscriptions/${subscriptionId}:expire`,
      { auth: "apiKey", body: { subscription_id: subscriptionId, reason } }
    );
  }
}
