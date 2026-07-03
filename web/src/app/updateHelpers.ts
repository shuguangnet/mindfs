import type { UpdateState } from "../services/update";

export function normalizeUpdateState(
  input: UpdateState | null | undefined,
): UpdateState {
  return {
    current_version: input?.current_version || "",
    latest_version: input?.latest_version || "",
    has_update: input?.has_update === true,
    status: input?.status || "idle",
    message: input?.message || "",
    release_name: input?.release_name || "",
    release_body: input?.release_body || "",
    release_url: input?.release_url || "",
    published_at: input?.published_at || "",
    last_checked_at: input?.last_checked_at || "",
    auto_update_supported: input?.auto_update_supported === true,
  };
}

export function updateButtonLabel(state: UpdateState): string {
  const status = (state.status || "idle").toLowerCase();
  switch (status) {
    case "available":
      if (state.current_version && state.latest_version) {
        return `更新 ${state.current_version} → ${state.latest_version}`;
      }
      return state.latest_version ? `更新到 ${state.latest_version}` : "新版本";
    case "downloading":
      return "下载中...";
    case "installing":
      return "安装中...";
    case "restarting":
      return "重启中...";
    case "failed":
      return "更新失败";
    default:
      return "已是最新";
  }
}

export function updateSummaryText(state: UpdateState): string {
  const body = String(state.release_body || "").trim();
  if (body) {
    return body;
  }
  const name = String(state.release_name || "").trim();
  if (name) {
    return name;
  }
  if (state.latest_version) {
    return `发现 v${state.latest_version} 新版本`;
  }
  return "";
}

export function shouldShowUpdateButton(state: UpdateState): boolean {
  const status = (state.status || "idle").toLowerCase();
  if (
    status === "downloading" ||
    status === "installing" ||
    status === "restarting" ||
    status === "failed"
  ) {
    return true;
  }
  return state.auto_update_supported === true && state.has_update === true;
}
