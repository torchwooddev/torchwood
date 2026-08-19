import { api } from "./client";
import type { ApiRequestConfig } from "./client";

export interface SubscriptionPlan {
  id: string;
  project_id?: string;
  code: string;
  name: string;
  amount: string;
  currency: string;
  interval: string;
  interval_days?: string;
  grace_days?: number;
  trial_days?: number;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface Subscription {
  id: string;
  project_id?: string;
  user_id?: string;
  plan_id?: string;
  plan_code?: string;
  mode: string;
  status: string;
  current_period_start?: string;
  current_period_end?: string;
  cancel_at_period_end?: boolean;
  grace_until?: string;
  created_at?: string;
}

export async function listPlans(): Promise<SubscriptionPlan[]> {
  const res = await api.get<{ plans: SubscriptionPlan[] }>("/server/subscriptions/plans");
  return res.data.plans ?? [];
}

export async function getPlan(planId: string): Promise<SubscriptionPlan> {
  const res = await api.get<SubscriptionPlan>(`/server/subscriptions/plans/${planId}`);
  return res.data;
}

export async function createPlan(input: {
  code: string;
  name: string;
  amount: string;
  currency: string;
  interval: string;
  interval_days?: string;
  grace_days?: number;
  trial_days?: number;
}): Promise<SubscriptionPlan> {
  const res = await api.post<SubscriptionPlan>("/server/subscriptions/plans", input);
  return res.data;
}

export async function updatePlan(
  planId: string,
  input: { name?: string; status?: string }
): Promise<SubscriptionPlan> {
  const res = await api.patch<SubscriptionPlan>(`/server/subscriptions/plans/${planId}`, {
    plan_id: planId,
    ...input,
  });
  return res.data;
}

export async function deletePlan(planId: string, config?: ApiRequestConfig): Promise<void> {
  await api.delete(`/server/subscriptions/plans/${planId}`, config);
}

export async function listSubscriptions(): Promise<Subscription[]> {
  const res = await api.get<{ subscriptions: Subscription[] }>("/server/subscriptions");
  return res.data.subscriptions ?? [];
}

export async function getSubscription(subscriptionId: string): Promise<Subscription> {
  const res = await api.get<Subscription>(`/server/subscriptions/${subscriptionId}`);
  return res.data;
}

export async function cancelSubscription(subscriptionId: string, reason?: string): Promise<Subscription> {
  const res = await api.post<Subscription>(`/server/subscriptions/${subscriptionId}:cancel`, {
    subscription_id: subscriptionId,
    reason,
  });
  return res.data;
}

export async function expireSubscription(subscriptionId: string, reason?: string): Promise<Subscription> {
  const res = await api.post<Subscription>(`/server/subscriptions/${subscriptionId}:expire`, {
    subscription_id: subscriptionId,
    reason,
  });
  return res.data;
}
