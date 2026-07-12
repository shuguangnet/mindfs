import { appPath } from "./base";
import { protectedJSON } from "./api";

export type RemoteServer = {
  id: string;
  name: string;
  base_url: string;
  node_id: string;
  pairing_secret?: string;
  default_root_id: string;
  enabled: boolean;
  has_secret?: boolean;
};

export type RemoteRoot = {
  id: string;
  name: string;
  display_name?: string;
  root_path?: string;
};

export type RemoteServerTestResult = {
  ok: boolean;
  roots: RemoteRoot[];
  agents: Array<Record<string, unknown>>;
  shells: Array<Record<string, unknown>>;
};

export async function fetchRemoteServers(): Promise<RemoteServer[]> {
  const payload = await protectedJSON<{ servers?: RemoteServer[] }>(appPath("/api/remote-servers"));
  return Array.isArray(payload.servers) ? payload.servers : [];
}

export async function saveRemoteServer(server: RemoteServer): Promise<RemoteServer> {
  return protectedJSON<RemoteServer>(appPath("/api/remote-servers"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(server),
  });
}

export async function deleteRemoteServer(id: string): Promise<void> {
  await protectedJSON(appPath(`/api/remote-servers/${encodeURIComponent(id)}`), {
    method: "DELETE",
  });
}

export async function testRemoteServer(id: string): Promise<RemoteServerTestResult> {
  return protectedJSON<RemoteServerTestResult>(appPath(`/api/remote-servers/${encodeURIComponent(id)}/test`), {
    method: "POST",
  });
}
