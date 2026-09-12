import { appPath } from "./base";

export type AuthStatus = {
  enabled: boolean;
  authenticated: boolean;
  username?: string;
  has_password?: boolean;
};

export type AuthSettings = {
  enabled: boolean;
  username: string;
  has_password: boolean;
};

export type AuthSettingsUpdate = {
  enabled?: boolean;
  username?: string;
  new_password?: string;
  current_password?: string;
};

export const AUTH_UNAUTHENTICATED_EVENT = "mindfs:unauthenticated";

function isUnauthenticatedPayload(payload: unknown): boolean {
  if (!payload || typeof payload !== "object") {
    return false;
  }
  const error = String((payload as { error?: unknown }).error || "").trim();
  return error === "unauthenticated";
}

async function parseJSON(response: Response): Promise<any> {
  const text = await response.text();
  if (!text) {
    return {};
  }
  try {
    return JSON.parse(text);
  } catch {
    return {};
  }
}

/**
 * Reads the auth status. This endpoint is exempt from the login gate so it can
 * be called before logging in. Session cookies are attached automatically for
 * same-origin requests.
 */
export async function fetchAuthStatus(): Promise<AuthStatus> {
  const response = await fetch(appPath("/api/auth/status"), {
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
  });
  const payload = await parseJSON(response);
  if (!response.ok) {
    // If the status endpoint is unreachable, fail open (no gate) so a broken
    // auth config cannot brick the whole UI.
    return { enabled: false, authenticated: true };
  }
  return {
    enabled: Boolean(payload.enabled),
    authenticated: Boolean(payload.authenticated),
    username: typeof payload.username === "string" ? payload.username : undefined,
    has_password: Boolean(payload.has_password),
  };
}

export class AuthError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string) {
    super(code || `auth_request_failed_${status}`);
    this.name = "AuthError";
    this.status = status;
    this.code = code;
  }
}

export async function loginAuth(
  username: string,
  password: string,
  remember: boolean,
): Promise<AuthStatus> {
  const response = await fetch(appPath("/api/auth/login"), {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password, remember }),
  });
  const payload = await parseJSON(response);
  if (!response.ok) {
    throw new AuthError(response.status, String(payload.error || ""));
  }
  return {
    enabled: Boolean(payload.enabled),
    authenticated: Boolean(payload.authenticated),
    username: typeof payload.username === "string" ? payload.username : undefined,
  };
}

export async function logoutAuth(): Promise<void> {
  try {
    await fetch(appPath("/api/auth/logout"), {
      method: "POST",
      credentials: "same-origin",
    });
  } catch {
    // Logout is best-effort; the UI returns to the login screen regardless.
  }
}

export async function fetchAuthSettings(): Promise<AuthSettings> {
  const response = await fetch(appPath("/api/auth/settings"), {
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
  });
  const payload = await parseJSON(response);
  if (!response.ok) {
    throw new AuthError(response.status, String(payload.error || ""));
  }
  return {
    enabled: Boolean(payload.enabled),
    username: typeof payload.username === "string" ? payload.username : "",
    has_password: Boolean(payload.has_password),
  };
}

export async function saveAuthSettings(update: AuthSettingsUpdate): Promise<AuthSettings> {
  const response = await fetch(appPath("/api/auth/settings"), {
    method: "PUT",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(update),
  });
  const payload = await parseJSON(response);
  if (!response.ok) {
    throw new AuthError(response.status, String(payload.error || ""));
  }
  return {
    enabled: Boolean(payload.enabled),
    username: typeof payload.username === "string" ? payload.username : "",
    has_password: Boolean(payload.has_password),
  };
}

export function notifyUnauthenticated(): void {
  if (typeof window === "undefined") {
    return;
  }
  window.dispatchEvent(new CustomEvent(AUTH_UNAUTHENTICATED_EVENT));
}

export function subscribeUnauthenticated(listener: () => void): () => void {
  if (typeof window === "undefined") {
    return () => {};
  }
  window.addEventListener(AUTH_UNAUTHENTICATED_EVENT, listener);
  return () => window.removeEventListener(AUTH_UNAUTHENTICATED_EVENT, listener);
}

export { isUnauthenticatedPayload };
