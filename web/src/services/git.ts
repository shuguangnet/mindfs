import { appURL } from "./base";
import { protectedJSON } from "./api";
import { getCachedGitDiff, setCachedGitDiff, type CachedGitDiffPayload } from "./file";

export type GitStatusCode = "M" | "A" | "D" | "R" | "??";

export type GitStatusItem = {
  path: string;
  display_path?: string;
  old_path?: string;
  status: GitStatusCode;
  additions: number;
  deletions: number;
  is_dir?: boolean;
};

export type GitStatusPayload = {
  available: boolean;
  branch?: string;
  dirty_count: number;
  items: GitStatusItem[];
};

export type GitDiffPayload = CachedGitDiffPayload & {
  path: string;
  status: GitStatusCode | string;
  additions: number;
  deletions: number;
  content: string;
};

export type GitBranchItem = {
  name: string;
  current: boolean;
};

export type GitBranchesPayload = {
  current?: string;
  branches: GitBranchItem[];
};

export async function fetchGitStatus(rootId: string): Promise<GitStatusPayload> {
  const payload = await protectedJSON<any>(appURL("/api/git/status", new URLSearchParams({ root: rootId })));
  return {
    available: payload?.available === true,
    branch: typeof payload?.branch === "string" ? payload.branch : undefined,
    dirty_count: Number(payload?.dirty_count) || 0,
    items: Array.isArray(payload?.items) ? payload.items as GitStatusItem[] : [],
  };
}

export async function fetchGitBranches(rootId: string): Promise<GitBranchesPayload> {
  const payload = await protectedJSON<any>(appURL("/api/git/branches", new URLSearchParams({ root: rootId })));
  return {
    current: typeof payload?.current === "string" ? payload.current : undefined,
    branches: Array.isArray(payload?.branches)
      ? payload.branches
          .map((item: any) => ({
            name: typeof item?.name === "string" ? item.name : "",
            current: item?.current === true,
          }))
          .filter((item: GitBranchItem) => !!item.name)
      : [],
  };
}

export async function createGitWorktree(input: {
  rootId: string;
  parentPath: string;
  name: string;
  branchMode: "new" | "existing";
  branch?: string;
}): Promise<any> {
  return protectedJSON<any>(appURL("/api/git/worktrees"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      root: input.rootId,
      parent_path: input.parentPath,
      name: input.name,
      branch_mode: input.branchMode,
      branch: input.branch || "",
    }),
  });
}

export async function removeGitWorktree(rootId: string): Promise<any> {
  return protectedJSON<any>(appURL("/api/git/worktrees"), {
    method: "DELETE",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ root: rootId }),
  });
}

export function buildGitDiffCacheSignature(item?: Partial<GitStatusItem> | null): string {
  if (!item) {
    return "";
  }
  return [
    item.status || "",
    item.old_path || "",
    Number(item.additions) || 0,
    Number(item.deletions) || 0,
  ].join(":");
}

export async function fetchGitDiff(
  rootId: string,
  path: string,
  options?: { cacheSignature?: string },
): Promise<GitDiffPayload> {
  const cacheSignature = options?.cacheSignature || "";
  const cached = await getCachedGitDiff(rootId, path, cacheSignature);
  if (cached) {
    return cached as GitDiffPayload;
  }

  const payload = await protectedJSON<any>(appURL("/api/git/diff", new URLSearchParams({ root: rootId, path })));
  const diff = {
    path: typeof payload?.path === "string" ? payload.path : path,
    status: typeof payload?.status === "string" ? payload.status : "M",
    additions: Number(payload?.additions) || 0,
    deletions: Number(payload?.deletions) || 0,
    content: typeof payload?.content === "string" ? payload.content : "",
    file_meta: Array.isArray(payload?.file_meta) ? payload.file_meta : [],
  };
  await setCachedGitDiff(rootId, path, diff, cacheSignature);
  return diff;
}
