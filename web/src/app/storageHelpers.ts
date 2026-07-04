export function loadPersistedFileScrollPositions(storageKey: string): Record<string, number> {
  if (typeof window === "undefined") {
    return {};
  }
  try {
    const raw = window.localStorage.getItem(storageKey);
    if (!raw) {
      return {};
    }
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return {};
    }
    const next: Record<string, number> = {};
    Object.entries(parsed).forEach(([key, value]) => {
      const scrollTop = Number(value);
      if (!key || !Number.isFinite(scrollTop) || scrollTop < 0) {
        return;
      }
      next[key] = scrollTop;
    });
    return next;
  } catch {
    return {};
  }
}

export function persistFileScrollPositions(
  storageKey: string,
  positions: Record<string, number>,
): void {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(storageKey, JSON.stringify(positions));
  } catch {}
}

export function loadLastRootId(storageKey: string): string {
  if (typeof window === "undefined") {
    return "";
  }
  return window.localStorage.getItem(storageKey) || "";
}

export function loadBooleanRecord(key: string): Record<string, boolean> {
  if (typeof window === "undefined") {
    return {};
  }
  try {
    const parsed = JSON.parse(window.localStorage.getItem(key) || "{}") as Record<string, unknown>;
    return Object.fromEntries(
      Object.entries(parsed).filter(([, value]) => typeof value === "boolean"),
    ) as Record<string, boolean>;
  } catch {
    return {};
  }
}

export function loadStringBooleanRecord(key: string): Record<string, Record<string, boolean>> {
  if (typeof window === "undefined") {
    return {};
  }
  try {
    const parsed = JSON.parse(window.localStorage.getItem(key) || "{}") as Record<string, unknown>;
    return Object.fromEntries(
      Object.entries(parsed).map(([root, value]) => [
        root,
        value && typeof value === "object"
          ? Object.fromEntries(
              Object.entries(value as Record<string, unknown>).filter(([, expanded]) => typeof expanded === "boolean"),
            )
          : {},
      ]),
    ) as Record<string, Record<string, boolean>>;
  } catch {
    return {};
  }
}

export function loadStoredBoolean(key: string): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  try {
    return window.localStorage.getItem(key) === "1";
  } catch {
    return false;
  }
}
