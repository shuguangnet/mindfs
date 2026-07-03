import React, { useEffect, useRef, useState } from "react";
import {
  fetchGitBranches,
  fetchGitRemoteSync,
  runGitCommitAndPush,
  runGitFetch,
  runGitPull,
  runGitPush,
  runGitPushUpstream,
  type GitBranchItem,
  type GitRemoteOperationPayload,
  type GitRemoteSyncPayload,
  type GitStatusItem,
  type GitStatusPayload,
} from "../services/git";

type GitStatusPanelProps = {
  rootId?: string;
  status: GitStatusPayload | null;
  loading?: boolean;
  isFiltered?: boolean;
  expanded?: boolean;
  onSelectItem?: (item: GitStatusItem) => void;
  onSwitchBranch?: (branch: string) => void | Promise<void>;
  onRepositoryChanged?: () => void | Promise<void>;
  onExpandedChange?: (expanded: boolean) => void;
};

function renderStatusColor(status: string): string {
  switch (status) {
    case "A":
      return "#15803d";
    case "D":
      return "#b91c1c";
    case "R":
      return "#1d4ed8";
    case "??":
      return "#7c3aed";
    default:
      return "#b45309";
  }
}

function renderLineStat(value: number, prefix: "+" | "-"): React.ReactNode {
  const color = prefix === "+" ? "#15803d" : "#b91c1c";
  return (
    <span style={{ color, fontVariantNumeric: "tabular-nums" }}>
      {prefix}{value}
    </span>
  );
}

function renderStatusLabel(status: string): string {
  if (status === "??") {
    return "U";
  }
  return status;
}

function operationLabel(result?: string): string {
  switch (result) {
    case "fetched":
      return "已获取远端更新";
    case "fast_forwarded":
      return "已拉取";
    case "up_to_date":
      return "已是最新";
    case "pushed":
      return "已推送";
    case "committed_and_pushed":
      return "已提交并推送";
    case "committed_push_failed":
      return "已提交，推送失败";
    case "blocked_dirty":
      return "有未提交变更，无法拉取";
    case "blocked_no_upstream":
      return "未设置 upstream";
    case "blocked_detached_head":
      return "当前不是分支";
    case "blocked_non_ff":
    case "rejected_non_ff":
      return "远端有新提交";
    case "blocked_empty_message":
      return "提交信息不能为空";
    case "blocked_no_changes":
      return "没有可提交变更";
    case "blocked_invalid_paths":
      return "提交范围无效";
    case "failed_auth":
      return "认证失败";
    case "failed_network":
      return "网络失败";
    case "failed_remote":
      return "远端不可用";
    case "failed":
      return "操作失败";
    default:
      return "";
  }
}

function shortHash(hash?: string): string {
  return hash ? hash.slice(0, 8) : "";
}

function IconButton({ title, disabled, busy, onClick, children }: { title: string; disabled?: boolean; busy?: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      aria-label={title}
      title={title}
      disabled={disabled || busy}
      onClick={onClick}
      style={{
        width: "26px",
        height: "26px",
        border: "1px solid var(--border-color)",
        borderRadius: "7px",
        background: "var(--panel-bg)",
        color: "var(--text-primary)",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        cursor: disabled || busy ? "default" : "pointer",
        opacity: disabled ? 0.45 : 1,
        padding: 0,
      }}
    >
      {busy ? <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>...</span> : children}
    </button>
  );
}

export function GitStatusPanel({ rootId, status, loading = false, isFiltered = false, expanded = true, onSelectItem, onSwitchBranch, onRepositoryChanged, onExpandedChange }: GitStatusPanelProps) {
  const branchMenuRef = useRef<HTMLDivElement | null>(null);
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [branches, setBranches] = useState<GitBranchItem[]>([]);
  const [branchesLoading, setBranchesLoading] = useState(false);
  const [switchingBranch, setSwitchingBranch] = useState("");
  const [remoteSync, setRemoteSync] = useState<GitRemoteSyncPayload | null>(null);
  const [remoteLoading, setRemoteLoading] = useState(false);
  const [operation, setOperation] = useState("");
  const [operationResult, setOperationResult] = useState<GitRemoteOperationPayload | null>(null);
  const [upstreamRemote, setUpstreamRemote] = useState("");
  const [upstreamBranch, setUpstreamBranch] = useState("");
  const [commitMessage, setCommitMessage] = useState("");
  const [commitScope, setCommitScope] = useState<"all" | "selected">("all");
  const [includeUntracked, setIncludeUntracked] = useState(true);
  const [selectedPaths, setSelectedPaths] = useState<Record<string, boolean>>({});

  useEffect(() => {
    if (!branchMenuOpen) {
      return;
    }
    const handlePointerDown = (event: MouseEvent) => {
      if (!branchMenuRef.current?.contains(event.target as Node)) {
        setBranchMenuOpen(false);
      }
    };
    document.addEventListener("mousedown", handlePointerDown);
    return () => document.removeEventListener("mousedown", handlePointerDown);
  }, [branchMenuOpen]);

  useEffect(() => {
    if (!branchMenuOpen || !rootId) {
      return;
    }
    let cancelled = false;
    setBranchesLoading(true);
    void fetchGitBranches(rootId)
      .then((payload) => {
        if (!cancelled) {
          setBranches(payload.branches || []);
        }
      })
      .catch((err) => {
        console.error("[git.branches] failed", { rootId, err });
        if (!cancelled) {
          setBranches([]);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setBranchesLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [branchMenuOpen, rootId]);

  useEffect(() => {
    if (!rootId || status?.available !== true) {
      setRemoteSync(null);
      return;
    }
    let cancelled = false;
    setRemoteLoading(true);
    void fetchGitRemoteSync(rootId)
      .then((payload) => {
        if (!cancelled) {
          setRemoteSync(payload);
          const firstRemote = payload.remotes[0]?.name || "origin";
          setUpstreamRemote((current) => current || payload.upstream_remote || firstRemote);
          setUpstreamBranch((current) => current || payload.current_branch || status?.branch || "main");
        }
      })
      .catch((err) => {
        console.error("[git.remote.sync] failed", { rootId, err });
        if (!cancelled) {
          setRemoteSync(null);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setRemoteLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [rootId, status?.available, status?.branch, status?.dirty_count]);

  useEffect(() => {
    const next: Record<string, boolean> = {};
    for (const item of status?.items || []) {
      next[item.path] = selectedPaths[item.path] !== false;
    }
    setSelectedPaths(next);
  }, [status?.items]);

  if (!loading && (!status || status.available !== true)) {
    return null;
  }

  const items = status?.items || [];
  const remotes = remoteSync?.remotes || [];
  const upstreamMissing = remoteSync?.state === "no_upstream";
  const remoteBusy = !!operation || remoteLoading;
  const selectedCommitPaths = items.filter((item) => selectedPaths[item.path]).map((item) => item.path);
  const refreshRemote = async () => {
    if (!rootId) {
      return;
    }
    const next = await fetchGitRemoteSync(rootId);
    setRemoteSync(next);
    if (next.upstream_remote) {
      setUpstreamRemote(next.upstream_remote);
    }
    if (next.upstream_branch) {
      setUpstreamBranch(next.upstream_branch);
    }
  };
  const runOperation = async (name: string, action: () => Promise<GitRemoteOperationPayload>) => {
    if (!rootId || operation) {
      return;
    }
    setOperation(name);
    setOperationResult(null);
    try {
      const result = await action();
      setOperationResult(result);
      if (result.state) {
        setRemoteSync(result.state);
      } else {
        await refreshRemote();
      }
      if (["fetched", "fast_forwarded", "pushed", "committed_and_pushed", "committed_push_failed"].includes(result.result)) {
        await onRepositoryChanged?.();
        if (result.result === "committed_and_pushed") {
          setCommitMessage("");
        }
      }
    } catch (err) {
      console.error("[git.remote.operation] failed", { rootId, name, err });
      setOperationResult({ result: "failed", message: err instanceof Error ? err.message : String(err) });
    } finally {
      setOperation("");
    }
  };
  const canCommit = commitMessage.trim() !== "" && items.length > 0 && (commitScope === "all" || selectedCommitPaths.length > 0);
  const commitTargetRemote = upstreamMissing ? upstreamRemote.trim() : "";
  const commitTargetBranch = upstreamMissing ? upstreamBranch.trim() : "";
  const resultText = operationLabel(operationResult?.result);
  const resultDetail = operationResult?.commit_hash ? shortHash(operationResult.commit_hash) : operationResult?.message || "";
  const remoteSummary = remoteSync?.upstream || remotes[0]?.name || "";
  const aheadBehind = remoteSync ? `${remoteSync.ahead > 0 ? `↑${remoteSync.ahead}` : ""}${remoteSync.behind > 0 ? ` ↓${remoteSync.behind}` : ""}`.trim() : "";
  if (!loading && items.length === 0 && !remoteSync && !remoteLoading) {
    return null;
  }

  return (
    <section style={{ padding: 0, flexShrink: 0 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "12px", marginBottom: "10px", padding: "0 10px" }}>
        <div style={{ display: "flex", alignItems: "center", gap: "8px", minWidth: 0 }}>
          <span
            title="Git 变更"
            aria-label="Git 变更"
            style={{
              width: "18px",
              height: "18px",
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              color: "var(--text-primary)",
              flexShrink: 0,
            }}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <path
                fill="currentColor"
                d="M7 5a2 2 0 1 1 3.763.945h.58a4 4 0 0 1 4 4v1.28a2 2 0 0 1-1.02 3.72a2 2 0 0 1-.98-3.745V9.945a2 2 0 0 0-2-2H10v9.323A2 2 0 0 1 9 21a2 2 0 0 1-1-3.732V6.732A2 2 0 0 1 7 5"
              />
            </svg>
          </span>
          {status?.branch ? (
            <div ref={branchMenuRef} style={{ position: "relative", minWidth: 0 }}>
              <button
                type="button"
                onClick={() => setBranchMenuOpen((open) => !open)}
                disabled={!rootId || !onSwitchBranch}
                style={{
                  border: "none",
                  background: branchMenuOpen ? "rgba(15, 23, 42, 0.06)" : "transparent",
                  color: "var(--text-primary)",
                  borderRadius: "7px",
                  padding: "3px 6px",
                  display: "inline-flex",
                  alignItems: "center",
                  gap: "4px",
                  minWidth: 0,
                  maxWidth: "180px",
                  cursor: rootId && onSwitchBranch ? "pointer" : "default",
                }}
              >
                <span style={{ fontSize: "12px", fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {status.branch}
                </span>
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 20 20"
                  fill="currentColor"
                  aria-hidden="true"
                  style={{
                    color: "var(--text-secondary)",
                    flexShrink: 0,
                    transform: branchMenuOpen ? "rotate(180deg)" : "rotate(0deg)",
                    transition: "transform 0.15s",
                  }}
                >
                  <path
                    fillRule="evenodd"
                    d="M5.23 7.21a.75.75 0 0 1 1.06.02L10 11.17l3.71-3.94a.75.75 0 1 1 1.08 1.04l-4.25 4.5a.75.75 0 0 1-1.08 0l-4.25-4.5a.75.75 0 0 1 .02-1.06"
                    clipRule="evenodd"
                  />
                </svg>
              </button>
              {branchMenuOpen ? (
                <div
                  style={{
                    position: "absolute",
                    top: "calc(100% + 6px)",
                    left: 0,
                    minWidth: "180px",
                    maxWidth: "260px",
                    maxHeight: "260px",
                    overflow: "auto",
                    padding: "6px",
                    borderRadius: "10px",
                    border: "1px solid var(--border-color)",
                    background: "var(--menu-bg)",
                    boxShadow: "0 12px 30px rgba(15, 23, 42, 0.14)",
                    zIndex: 25,
                  }}
                >
                  {branchesLoading ? (
                    <div style={{ padding: "8px 10px", fontSize: "12px", color: "var(--text-secondary)" }}>加载中...</div>
                  ) : branches.length === 0 ? (
                    <div style={{ padding: "8px 10px", fontSize: "12px", color: "var(--text-secondary)" }}>无可切换分支</div>
                  ) : branches.map((branch) => {
                    const active = branch.name === status.branch;
                    const busy = switchingBranch === branch.name;
                    return (
                      <button
                        key={branch.name}
                        type="button"
                        disabled={active || !!switchingBranch}
                        onClick={async () => {
                          setSwitchingBranch(branch.name);
                          try {
                            await onSwitchBranch?.(branch.name);
                            setBranchMenuOpen(false);
                          } finally {
                            setSwitchingBranch("");
                          }
                        }}
                        style={{
                          width: "100%",
                          border: "none",
                          background: active ? "var(--selection-bg)" : "transparent",
                          color: active ? "var(--accent-color)" : "var(--text-primary)",
                          borderRadius: "8px",
                          padding: "8px 10px",
                          display: "flex",
                          alignItems: "center",
                          justifyContent: "space-between",
                          gap: "12px",
                          textAlign: "left",
                          cursor: active || switchingBranch ? "default" : "pointer",
                          fontSize: "12px",
                          opacity: switchingBranch && !busy ? 0.58 : 1,
                        }}
                      >
                        <span style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                          {branch.name}
                        </span>
                        <span style={{ fontSize: "11px", color: "var(--text-secondary)", flexShrink: 0 }}>
                          {busy ? "..." : active ? "✓" : ""}
                        </span>
                      </button>
                    );
                  })}
                </div>
              ) : null}
            </div>
          ) : null}
          {remoteSummary ? (
            <span
              title={remoteSummary}
              style={{
                fontSize: "11px",
                color: "var(--text-secondary)",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
                maxWidth: "160px",
              }}
            >
              {remoteSummary}{aheadBehind ? ` ${aheadBehind}` : ""}
            </span>
          ) : remoteLoading ? (
            <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>...</span>
          ) : null}
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: "6px", flexShrink: 0 }}>
          <IconButton
            title="Fetch"
            disabled={!rootId || !remoteSync?.available}
            busy={operation === "fetch"}
            onClick={() => void runOperation("fetch", () => runGitFetch(rootId || "", remoteSync?.upstream_remote || remotes[0]?.name || ""))}
          >
            <svg width="14" height="14" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path d="M10 3a1 1 0 0 1 1 1v7.59l2.3-2.3a1 1 0 1 1 1.4 1.42l-4 4a1 1 0 0 1-1.4 0l-4-4a1 1 0 1 1 1.4-1.42L9 11.6V4a1 1 0 0 1 1-1Z" />
              <path d="M4 15a1 1 0 0 1 1-1h10a1 1 0 1 1 0 2H5a1 1 0 0 1-1-1Z" />
            </svg>
          </IconButton>
          <IconButton
            title="Pull"
            disabled={!rootId || !remoteSync?.available || upstreamMissing || remoteSync?.dirty}
            busy={operation === "pull"}
            onClick={() => void runOperation("pull", () => runGitPull(rootId || ""))}
          >
            <svg width="14" height="14" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path d="M10 2a1 1 0 0 1 1 1v8.58l2.3-2.29a1 1 0 0 1 1.4 1.42l-4 4a1 1 0 0 1-1.4 0l-4-4a1 1 0 1 1 1.4-1.42L9 11.6V3a1 1 0 0 1 1-1Z" />
              <path d="M4 17a1 1 0 0 1 1-1h10a1 1 0 1 1 0 2H5a1 1 0 0 1-1-1Z" />
            </svg>
          </IconButton>
          <IconButton
            title="Push"
            disabled={!rootId || !remoteSync?.available || upstreamMissing || (remoteSync?.ahead || 0) <= 0}
            busy={operation === "push"}
            onClick={() => void runOperation("push", () => runGitPush(rootId || ""))}
          >
            <svg width="14" height="14" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path d="M10.7 4.29a1 1 0 0 0-1.4 0l-4 4a1 1 0 1 0 1.4 1.42L9 7.4V16a1 1 0 1 0 2 0V7.4l2.3 2.31a1 1 0 1 0 1.4-1.42l-4-4Z" />
              <path d="M4 3a1 1 0 0 1 1-1h10a1 1 0 1 1 0 2H5a1 1 0 0 1-1-1Z" />
            </svg>
          </IconButton>
          <div style={{ fontSize: "11px", fontWeight: 700, color: "var(--text-secondary)" }}>
            {loading ? "..." : items.length}
          </div>
          <button
            type="button"
            aria-label={expanded ? "收起 Git 变更" : "展开 Git 变更"}
            title={expanded ? "收起" : "展开"}
            onClick={() => onExpandedChange?.(!expanded)}
            style={{
              width: "22px",
              height: "22px",
              border: "none",
              borderRadius: "7px",
              background: "transparent",
              color: "var(--text-secondary)",
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              cursor: "pointer",
              padding: 0,
            }}
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 20 20"
              fill="currentColor"
              aria-hidden="true"
              style={{
                transform: expanded ? "rotate(180deg)" : "rotate(0deg)",
                transition: "transform 0.15s",
              }}
            >
              <path
                fillRule="evenodd"
                d="M5.23 7.21a.75.75 0 0 1 1.06.02L10 11.17l3.71-3.94a.75.75 0 1 1 1.08 1.04l-4.25 4.5a.75.75 0 0 1-1.08 0l-4.25-4.5a.75.75 0 0 1 .02-1.06"
                clipRule="evenodd"
              />
            </svg>
          </button>
        </div>
      </div>

      {!expanded ? null : loading ? (
        <div style={{ fontSize: "12px", color: "var(--text-secondary)", padding: "6px 10px" }}>正在加载 git 变更...</div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: "8px", paddingLeft: "14px" }}>
          {operationResult ? (
            <div style={{ fontSize: "12px", color: operationResult.result === "committed_push_failed" || operationResult.result.startsWith("failed") || operationResult.result.startsWith("blocked") || operationResult.result.startsWith("rejected") ? "#b91c1c" : "#15803d", padding: "0 10px" }}>
              {resultText}{resultDetail ? ` · ${resultDetail}` : ""}
            </div>
          ) : null}
          {upstreamMissing && remotes.length > 0 ? (
            <div style={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr) auto", gap: "6px", padding: "0 10px" }}>
              <select
                value={upstreamRemote}
                onChange={(event) => setUpstreamRemote(event.target.value)}
                disabled={remoteBusy}
                style={{ minWidth: 0, height: "28px", border: "1px solid var(--border-color)", borderRadius: "7px", background: "var(--panel-bg)", color: "var(--text-primary)", fontSize: "12px" }}
              >
                {remotes.map((remote) => <option key={remote.name} value={remote.name}>{remote.name}</option>)}
              </select>
              <input
                value={upstreamBranch}
                onChange={(event) => setUpstreamBranch(event.target.value)}
                disabled={remoteBusy}
                placeholder={status?.branch || "branch"}
                style={{ minWidth: 0, height: "28px", border: "1px solid var(--border-color)", borderRadius: "7px", background: "var(--panel-bg)", color: "var(--text-primary)", fontSize: "12px", padding: "0 8px" }}
              />
              <button
                type="button"
                disabled={remoteBusy || !upstreamRemote.trim() || !upstreamBranch.trim()}
                onClick={() => void runOperation("upstream", () => runGitPushUpstream(rootId || "", upstreamRemote.trim(), upstreamBranch.trim()))}
                style={{ height: "28px", border: "1px solid var(--border-color)", borderRadius: "7px", background: "var(--panel-bg)", color: "var(--text-primary)", fontSize: "12px", padding: "0 8px", cursor: remoteBusy ? "default" : "pointer" }}
              >
                发布
              </button>
            </div>
          ) : null}
          {items.length > 0 ? (
            <div style={{ display: "flex", flexDirection: "column", gap: "6px", padding: "0 10px" }}>
              <input
                value={commitMessage}
                onChange={(event) => setCommitMessage(event.target.value)}
                disabled={remoteBusy}
                placeholder="Commit message"
                style={{ height: "30px", border: "1px solid var(--border-color)", borderRadius: "7px", background: "var(--panel-bg)", color: "var(--text-primary)", fontSize: "12px", padding: "0 9px" }}
              />
              <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "8px" }}>
                <div style={{ display: "inline-flex", border: "1px solid var(--border-color)", borderRadius: "7px", overflow: "hidden", flexShrink: 0 }}>
                  {(["all", "selected"] as const).map((mode) => (
                    <button
                      key={mode}
                      type="button"
                      onClick={() => setCommitScope(mode)}
                      disabled={remoteBusy}
                      style={{ border: "none", background: commitScope === mode ? "var(--selection-bg)" : "var(--panel-bg)", color: commitScope === mode ? "var(--accent-color)" : "var(--text-secondary)", fontSize: "11px", padding: "5px 8px", cursor: remoteBusy ? "default" : "pointer" }}
                    >
                      {mode === "all" ? "全部" : "选择"}
                    </button>
                  ))}
                </div>
                <label style={{ display: "inline-flex", alignItems: "center", gap: "5px", fontSize: "11px", color: "var(--text-secondary)", minWidth: 0 }}>
                  <input type="checkbox" checked={includeUntracked} disabled={remoteBusy} onChange={(event) => setIncludeUntracked(event.target.checked)} />
                  Untracked
                </label>
                <button
                  type="button"
                  disabled={remoteBusy || !canCommit || (upstreamMissing && (!commitTargetRemote || !commitTargetBranch))}
                  onClick={() => void runOperation("commit-push", () => runGitCommitAndPush({
                    rootId: rootId || "",
                    message: commitMessage,
                    all: commitScope === "all",
                    paths: commitScope === "selected" ? selectedCommitPaths : [],
                    includeUntracked,
                    remote: commitTargetRemote,
                    branch: commitTargetBranch,
                  }))}
                  style={{ height: "28px", border: "1px solid var(--border-color)", borderRadius: "7px", background: "var(--panel-bg)", color: "var(--text-primary)", fontSize: "12px", padding: "0 9px", cursor: remoteBusy || !canCommit ? "default" : "pointer", opacity: remoteBusy || !canCommit ? 0.55 : 1, flexShrink: 0 }}
                >
                  Commit & Push
                </button>
              </div>
            </div>
          ) : null}
          {items.map((item) => (
            <button
              key={`${item.status}:${item.path}`}
              type="button"
              disabled={item.is_dir === true}
              onClick={() => {
                if (item.is_dir !== true) {
                  onSelectItem?.(item);
                }
              }}
              style={{
                display: "flex",
                alignItems: "center",
                gap: "10px",
                width: "100%",
                border: "none",
                background: "linear-gradient(180deg, rgba(59, 130, 246, 0.08), rgba(59, 130, 246, 0.03))",
                padding: "6px 10px",
                cursor: item.is_dir === true ? "default" : "pointer",
                textAlign: "left",
                borderRadius: "8px",
                transition: "background 0.15s",
                opacity: item.is_dir === true ? 0.72 : 1,
              }}
              onMouseEnter={(e) => { e.currentTarget.style.background = "linear-gradient(180deg, rgba(59, 130, 246, 0.12), rgba(59, 130, 246, 0.05))"; }}
              onMouseLeave={(e) => { e.currentTarget.style.background = "linear-gradient(180deg, rgba(59, 130, 246, 0.08), rgba(59, 130, 246, 0.03))"; }}
            >
              <span style={{ width: "24px", color: renderStatusColor(item.status), fontSize: "12px", fontWeight: 700, flexShrink: 0 }}>
                {renderStatusLabel(item.status)}
              </span>
              {commitScope === "selected" ? (
                <input
                  type="checkbox"
                  checked={selectedPaths[item.path] !== false}
                  onClick={(event) => event.stopPropagation()}
                  onChange={(event) => setSelectedPaths((prev) => ({ ...prev, [item.path]: event.target.checked }))}
                  style={{ flexShrink: 0 }}
                />
              ) : null}
              <span style={{ flex: 1, minWidth: 0, fontSize: "12px", color: "var(--text-primary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                {item.display_path || item.path}
              </span>
              <span style={{ display: "inline-flex", alignItems: "center", gap: "8px", fontSize: "11px", color: "var(--text-secondary)", flexShrink: 0 }}>
                {renderLineStat(item.additions, "+")}
                {renderLineStat(item.deletions, "-")}
              </span>
            </button>
          ))}
        </div>
      )}
    </section>
  );
}
