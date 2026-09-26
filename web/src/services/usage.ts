import { protectedJSON } from "./api";
import { appURL } from "./base";

export type UsageTotals = {
  turns: number;
  input: number;
  output: number;
  cache_read: number;
  cache_write: number;
  total_tokens: number;
  cost_usd: number;
  unpriced_runs: number;
};

export type UsageDayBucket = UsageTotals & {
  date: string;
};

export type UsageGroupBucket = UsageTotals & {
  key: string;
  label: string;
  root_id?: string;
};

export type UsageBudget = {
  daily_usd: number;
  notify_percent?: number;
  notify_on_exceed?: boolean;
};

export type UsageReport = {
  from: string;
  to: string;
  days: number;
  totals: UsageTotals;
  today: UsageTotals;
  budget: UsageBudget;
  budget_used_usd: number;
  budget_ratio: number;
  budget_exceeded: boolean;
  by_day: UsageDayBucket[];
  by_agent: UsageGroupBucket[];
  by_model: UsageGroupBucket[];
  by_project: UsageGroupBucket[];
  previous_totals: UsageTotals;
  partial?: boolean;
  skipped_files?: number;
  skipped_reason?: string;
  unpriced_models?: string[];
  price_configured: boolean;
  record_count: number;
  scanned_file_count: number;
};

export type UsagePrice = {
  input: number;
  output: number;
  cache_read?: number;
  cache_write?: number;
};

export type UsagePreferences = {
  prices: Record<string, UsagePrice>;
  budget: UsageBudget;
};

export async function fetchUsageReport(
  options: { root?: string; days?: number } = {},
): Promise<UsageReport> {
  const params = new URLSearchParams();
  if (options.root) params.set("root", options.root);
  if (options.days && options.days > 0) params.set("days", String(options.days));
  return protectedJSON<UsageReport>(appURL("/api/usage/report", params));
}

export async function fetchUsagePreferences(): Promise<UsagePreferences> {
  return protectedJSON<UsagePreferences>(appURL("/api/usage/preferences"));
}

export async function saveUsagePreferences(
  prefs: UsagePreferences,
): Promise<UsagePreferences> {
  return protectedJSON<UsagePreferences>(appURL("/api/usage/preferences"), {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(prefs),
  });
}
