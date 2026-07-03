import { storeRelayNodes } from "../services/launcherNodeSync";
import { isNativeShellRuntime } from "../services/runtime";

export type RelayManagedRoot = {
  id: string;
  display_name?: string;
  root_path?: string;
};

export function relayNodeIdFromPathname(pathname: string): string {
  const match = /^\/n\/([^/]+)/.exec(String(pathname || ""));
  return match?.[1] || "";
}

function isStandaloneDisplayMode(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  const navigatorWithStandalone = navigator as Navigator & {
    standalone?: boolean;
  };
  return (
    window.matchMedia?.("(display-mode: standalone)")?.matches === true ||
    navigatorWithStandalone.standalone === true
  );
}

export function isRelayPWAContext(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  return (
    isStandaloneDisplayMode() &&
    (/^\/n\/[^/]+/.test(window.location.pathname) ||
      window.location.pathname === "/nodes" ||
      window.location.pathname === "/login")
  );
}

export function isRelayNodesPage(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  return (window.location.pathname.replace(/\/+$/, "") || "/") === "/nodes";
}

function relayNodeURL(rootID: string): string {
  if (typeof window === "undefined") {
    return "";
  }
  const trimmed = String(rootID || "").trim();
  if (!trimmed) {
    return "";
  }
  return new URL(`/n/${encodeURIComponent(trimmed)}/`, window.location.origin).toString();
}

export function syncRelayNodesToNative(dirs: RelayManagedRoot[]): void {
  if ((!isNativeShellRuntime() && !isRelayPWAContext()) || !isRelayNodesPage()) {
    return;
  }
  const nodes = dirs
    .map((dir) => {
      const id = String(dir.id || "").trim();
      const url = relayNodeURL(id);
      const name = String(
        dir.display_name || dir.root_path?.split("/").filter(Boolean).pop() || id,
      ).trim();
      if (!id || !url || !name) {
        return null;
      }
      return { name, url };
    })
    .filter((node): node is { name: string; url: string } => node !== null);
  void storeRelayNodes(nodes);
}
