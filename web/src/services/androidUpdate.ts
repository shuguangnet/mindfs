import { Capacitor } from "@capacitor/core";
import { isCapacitorRuntime } from "./runtime";

const DEFAULT_ANDROID_VERSION_URL = "https://relay.a9gent.com/api/versions/android";

export type AndroidUpdateState = {
  current_version: string;
  current_build: string;
  latest_version: string;
  has_update: boolean;
  status: "idle" | "available" | "downloading" | "downloaded" | "failed";
  message: string;
  notes: string;
  download_url: string;
  filename: string;
};

type RelayVersionResponse = {
  version?: string;
  notes?: string;
  downloads?: Array<{
    os?: string;
    arch?: string;
    filename?: string;
    url?: string;
  }>;
};

type WindowWithAppInfoBridge = Window & {
  MindFSAppInfo?: {
    getInfo?: () => string;
  };
};

export function normalizeAndroidUpdateState(
  input: Partial<AndroidUpdateState> | null | undefined,
): AndroidUpdateState {
  return {
    current_version: input?.current_version || "",
    current_build: input?.current_build || "",
    latest_version: input?.latest_version || "",
    has_update: input?.has_update === true,
    status: input?.status || "idle",
    message: input?.message || "",
    notes: input?.notes || "",
    download_url: input?.download_url || "",
    filename: input?.filename || "",
  };
}

export function isAndroidRuntime(): boolean {
  return isCapacitorRuntime() && Capacitor.getPlatform() === "android";
}

export async function fetchAndroidUpdateState(): Promise<AndroidUpdateState> {
  if (!isAndroidRuntime()) {
    return normalizeAndroidUpdateState(null);
  }

  const appInfo = await getAppInfo();
  const endpoint = buildVersionEndpoint();
  const response = await fetch(endpoint, { cache: "no-cache" });
  if (!response.ok) {
    throw new Error(`android_version_check_failed_${response.status}`);
  }

  const payload = (await response.json()) as RelayVersionResponse;
  const download = pickAndroidDownload(payload);
  const latestVersion = normalizeVersionLabel(payload.version || "");
  const currentVersion = normalizeVersionLabel(appInfo.version || "");
  const hasUpdate =
    !!latestVersion &&
    !!download.url &&
    isNewerVersion(latestVersion, currentVersion);

  return normalizeAndroidUpdateState({
    current_version: appInfo.version || "",
    current_build: appInfo.build || "",
    latest_version: latestVersion,
    has_update: hasUpdate,
    status: hasUpdate ? "available" : "idle",
    notes: payload.notes || "",
    download_url: download.url || "",
    filename: download.filename || filenameFromURL(download.url || "") || "mindfs-android.apk",
  });
}

function buildVersionEndpoint(): string {
  const configured = String(import.meta.env.VITE_ANDROID_VERSION_URL || "").trim();
  if (configured) {
    return configured;
  }
  return DEFAULT_ANDROID_VERSION_URL;
}

async function getAppInfo(): Promise<{ version?: string; build?: string }> {
  const nativeBridge = (window as WindowWithAppInfoBridge).MindFSAppInfo;
  if (nativeBridge && typeof nativeBridge.getInfo === "function") {
    try {
      return JSON.parse(nativeBridge.getInfo()) as { version?: string; build?: string };
    } catch {
    }
  }
  const { App } = await import("@capacitor/app");
  return App.getInfo();
}

function pickAndroidDownload(payload: RelayVersionResponse): {
  filename?: string;
  url?: string;
} {
  const downloads = Array.isArray(payload.downloads) ? payload.downloads : [];
  return (
    downloads.find((item) => /\.apk(?:$|\?)/i.test(item.url || item.filename || "")) ||
    downloads[0] ||
    {}
  );
}

function normalizeVersionLabel(value: string): string {
  return String(value || "").trim().replace(/^v/i, "");
}

function isNewerVersion(latest: string, current: string): boolean {
  const latestParts = parseVersionParts(latest);
  const currentParts = parseVersionParts(current);
  if (!latestParts || !currentParts) {
    return latest !== current;
  }
  const length = Math.max(latestParts.length, currentParts.length);
  for (let i = 0; i < length; i += 1) {
    const left = latestParts[i] || 0;
    const right = currentParts[i] || 0;
    if (left > right) return true;
    if (left < right) return false;
  }
  return false;
}

function parseVersionParts(value: string): number[] | null {
  const match = normalizeVersionLabel(value).match(/^(\d+(?:\.\d+){0,3})/);
  if (!match) {
    return null;
  }
  return match[1].split(".").map((part) => Number.parseInt(part, 10));
}

function filenameFromURL(value: string): string {
  try {
    const parsed = new URL(value);
    const parts = parsed.pathname.split("/").filter(Boolean);
    return parts[parts.length - 1] || "";
  } catch {
    return "";
  }
}
