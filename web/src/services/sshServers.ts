import { appPath } from "./base";
import { protectedJSON } from "./api";

export type SSHAuthMode = "key_path" | "key_inline" | "password";

export type SSHServer = {
  id: string;
  alias: string;
  host: string;
  port: number;
  user: string;
  auth: SSHAuthMode;
  key_path?: string;
  key_source?: "path" | "managed" | "";
  has_password: boolean;
  proxy_jump?: string;
  enabled: boolean;
  notes?: string;
  updated_at?: string;
};

export type SSHSaveInput = {
  id: string;
  alias: string;
  host: string;
  port: number;
  user: string;
  auth: SSHAuthMode;
  key_path?: string;
  proxy_jump?: string;
  enabled: boolean;
  notes?: string;
  new_password?: string;
  new_key_content?: string;
  reuse_password_from?: string;
  reuse_key_from?: string;
};

export type SSHSaveResult = {
  server: SSHServer;
  warning?: string;
};

export type SSHReuseRef = { id: string; alias: string };

export type SSHReuseInfo = {
  key_paths: string[];
  passwords: SSHReuseRef[];
  managed_keys: SSHReuseRef[];
};

export type SSHListResponse = {
  servers: SSHServer[];
  reuse: SSHReuseInfo;
  materialize?: { config_file: string; included: boolean; warning?: string } | null;
};

export type SSHTestResult = {
  ok: boolean;
  reason_code?: string;
  banner?: string;
  detail?: string;
};

export type SSHDeployResult = {
  server: SSHServer;
  ok: boolean;
  detail?: string;
  warning?: string;
};

export type SSHImportMeta = {
  alias: string;
  host: string;
  port: number;
  user: string;
  auth: SSHAuthMode;
  key_path?: string;
  proxy_jump?: string;
  notes?: string;
  enabled: boolean;
};

export type SSHImportEntry = {
  meta: SSHImportMeta;
  has_secret: boolean;
  conflict: boolean;
  valid: boolean;
  error?: string;
};

export type SSHImportPreview = {
  source: "json" | "ssh_config";
  entries: SSHImportEntry[];
  skipped_patterns?: string[];
  skipped_matches: number;
  needs_passphrase: boolean;
};

export type SSHImportApplyResult = {
  created: number;
  updated: number;
  skipped: number;
  warning?: string;
};

export type SSHExportFile = {
  mindfs: string;
  version: number;
  mode: "encrypted" | "no-secrets";
  exported_at: string;
  servers: SSHImportMeta[];
  secrets?: unknown;
};

export type SSHFSEntry = {
  name: string;
  path: string;
  is_dir: boolean;
  is_link?: boolean;
  size?: number;
  looks_like_key?: boolean;
};

export type SSHFSListing = {
  path: string;
  parent?: string;
  home: string;
  entries: SSHFSEntry[];
  truncated?: boolean;
};

export async function fetchSSHServers(): Promise<SSHListResponse> {
  const payload = await protectedJSON<SSHListResponse>(appPath("/api/ssh-servers"));
  return {
    servers: Array.isArray(payload.servers) ? payload.servers : [],
    reuse: {
      key_paths: Array.isArray(payload.reuse?.key_paths) ? payload.reuse.key_paths : [],
      passwords: Array.isArray(payload.reuse?.passwords) ? payload.reuse.passwords : [],
      managed_keys: Array.isArray(payload.reuse?.managed_keys) ? payload.reuse.managed_keys : [],
    },
    materialize: payload.materialize ?? null,
  };
}

export async function saveSSHServer(input: SSHSaveInput): Promise<SSHSaveResult> {
  return protectedJSON<SSHSaveResult>(appPath("/api/ssh-servers"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export async function deleteSSHServer(id: string): Promise<void> {
  await protectedJSON(appPath(`/api/ssh-servers/${encodeURIComponent(id)}`), {
    method: "DELETE",
  });
}

export async function testSSHServer(id: string): Promise<SSHTestResult> {
  return protectedJSON<SSHTestResult>(appPath(`/api/ssh-servers/${encodeURIComponent(id)}/test`), {
    method: "POST",
  });
}

export async function deploySSHServerKey(id: string): Promise<SSHDeployResult> {
  return protectedJSON<SSHDeployResult>(appPath(`/api/ssh-servers/${encodeURIComponent(id)}/deploy-key`), {
    method: "POST",
  });
}

export async function previewSSHImport(
  source: "json" | "ssh_config",
  payload: string,
  passphrase = "",
): Promise<SSHImportPreview> {
  return protectedJSON<SSHImportPreview>(appPath("/api/ssh-servers/import/preview"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ source, payload, passphrase }),
  });
}

export async function applySSHImport(
  source: "json" | "ssh_config",
  payload: string,
  passphrase: string,
  resolutions: Record<string, "overwrite" | "suffix" | "skip">,
): Promise<SSHImportApplyResult> {
  return protectedJSON<SSHImportApplyResult>(appPath("/api/ssh-servers/import/apply"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ source, payload, passphrase, resolutions }),
  });
}

export async function exportSSHServers(mode: "encrypted" | "no-secrets", passphrase = ""): Promise<SSHExportFile> {
  return protectedJSON<SSHExportFile>(appPath("/api/ssh-servers/export"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ mode, passphrase }),
  });
}

export async function browseSSHKeyFS(path = ""): Promise<SSHFSListing> {
  const query = new URLSearchParams({ path });
  const payload = await protectedJSON<SSHFSListing>(appPath(`/api/ssh-servers/fs?${query.toString()}`));
  return {
    path: payload.path || "",
    parent: payload.parent || "",
    home: payload.home || "",
    entries: Array.isArray(payload.entries) ? payload.entries : [],
    truncated: payload.truncated ?? false,
  };
}

// Triggers a browser download for the export file content.
export function downloadSSHExportFile(file: SSHExportFile): void {
  const blob = new Blob([JSON.stringify(file, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const date = new Date();
  const stamp = `${date.getFullYear()}${String(date.getMonth() + 1).padStart(2, "0")}${String(date.getDate()).padStart(2, "0")}`;
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `mindfs-ssh-servers-${stamp}.json`;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
