import { appPath } from "./base";
import { e2eeService } from "./e2ee";

export type TokenStationInfo = {
  success?: boolean;
  message?: string;
  data?: {
    quota?: number;
    used_quota?: number;
    balance_text?: string;
    used_quota_text?: string;
    quota_display_text?: string;
    topup_url?: string;
  };
};

export type TokenStationBindStatus = {
  bound?: boolean;
  pending_code?: string;
  relay_base_url?: string;
  topup_url?: string;
  last_error?: string;
};

export async function fetchTokenStationInfo(): Promise<TokenStationInfo> {
  const target = appPath("/api/token-station/userinfo");
  const response = e2eeService.isRequired()
    ? await e2eeService.protectedFetch(target)
    : await fetch(target);
  if (!response.ok) {
    throw new Error(`token_station_info_failed_${response.status}`);
  }
  return e2eeService.parseProtectedJSONResponse<TokenStationInfo>(response);
}

export async function startTokenStationBinding(): Promise<TokenStationBindStatus> {
  const target = appPath("/api/token-station/bind/start");
  const init: RequestInit = { method: "POST" };
  const response = e2eeService.isRequired()
    ? await e2eeService.protectedFetch(target, init)
    : await fetch(target, init);
  if (!response.ok) {
    throw new Error(`token_station_bind_failed_${response.status}`);
  }
  return e2eeService.parseProtectedJSONResponse<TokenStationBindStatus>(response);
}
