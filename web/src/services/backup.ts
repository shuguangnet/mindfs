import { e2eeService } from "./e2ee";
import { protectedJSON } from "./api";
import { appPath } from "./base";

export type BackupPreviewRoot = {
  id: string;
  name: string;
  root_path: string;
  meta_location: string;
  status: "new" | "exists_same_path" | "path_missing_on_disk";
  file_count: number;
  bytes: number;
  has_tasks: boolean;
};

export type BackupPreview = {
  token: string;
  created_at: string;
  mindfs_version?: string;
  options: { include_uploads: boolean; include_device_identity: boolean };
  roots: BackupPreviewRoot[];
  global: { file_count: number; bytes: number; device_identity_included: boolean };
  warnings?: string[];
};

export type BackupApplyRoot = {
  id: string;
  action: "skip" | "merge" | "replace";
  path_override?: string;
};

export type BackupApplyResult = {
  roots: Array<{
    id: string;
    action: string;
    root_path: string;
    files_copied: number;
    sessions_merged: number;
    registered: boolean;
    path_overridden: boolean;
  }>;
  global_files_replaced: number;
  global_skipped: boolean;
  warnings?: string[];
  reload_needed: boolean;
};

export type NativeExportResult = {
  session_id: string;
  dir?: string;
  path?: string;
  existed?: boolean;
  mode: string;
};

export type BackupExportOptions = {
  passphrase: string;
  include_uploads: boolean;
  include_device_identity: boolean;
};

function apiURL(path: string): string {
  return appPath(path);
}

export async function exportBackupBundle(opts: BackupExportOptions): Promise<{ blob: Blob; filename: string }> {
  const response = await e2eeService.protectedFetch(apiURL("/api/backup/export"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(opts),
  });
  if (!response.ok) {
    let message = `export failed: ${response.status}`;
    try {
      const payload = await response.clone().json();
      if (payload?.error) message = String(payload.error);
    } catch {
      /* binary or empty body */
    }
    throw new Error(message);
  }
  const disposition = response.headers.get("Content-Disposition") || "";
  const match = /filename="?([^";]+)"?/.exec(disposition);
  const filename = match?.[1] || `mindfs-backup-${Date.now()}.mbk`;
  return { blob: await response.blob(), filename };
}

export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

export async function importBackupBundle(file: File, passphrase: string): Promise<BackupPreview> {
  const form = new FormData();
  form.append("file", file, file.name || "bundle.mbk");
  form.append("passphrase", passphrase);
  const response = await e2eeService.protectedFetch(apiURL("/api/backup/import"), {
    method: "POST",
    body: form,
  });
  const payload = await response.clone().json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(String(payload?.error || `import failed: ${response.status}`));
  }
  return payload as BackupPreview;
}

export async function applyBackupImport(
  token: string,
  roots: BackupApplyRoot[],
  global: "skip" | "replace",
  includeDeviceIdentity: boolean,
): Promise<BackupApplyResult> {
  return protectedJSON<BackupApplyResult>(apiURL("/api/backup/apply"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      token,
      roots,
      global,
      include_device_identity: includeDeviceIdentity,
    }),
  });
}

export async function exportSessionNative(
  rootId: string,
  sessionKey: string,
  mode: "write" | "download" = "write",
  agent?: string,
): Promise<NativeExportResult> {
  const params = new URLSearchParams({ root: rootId });
  return protectedJSON<NativeExportResult>(
    `${apiURL(`/api/sessions/${encodeURIComponent(sessionKey)}/export-native`)}?${params.toString()}`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mode, agent }),
    },
  );
}
