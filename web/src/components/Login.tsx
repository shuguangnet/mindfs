import React, { useEffect, useState, type ReactElement } from "react";
import {
  getStoredLauncherNodes,
  setStoredLauncherNodes,
  type LauncherNode,
} from "../services/storage";
import {
  consumePendingRelayNodes,
  getNativeLauncherNodes,
  setNativeLauncherNodes,
} from "../services/launcherNodeSync";
import {
  fetchAndroidUpdateState,
  isAndroidRuntime,
  normalizeAndroidUpdateState,
  type AndroidUpdateState,
} from "../services/androidUpdate";
import { downloadURL } from "../services/download";

type LoginProps = {
  onOpenNode: (nodeURL: string) => void;
};

const RELAY_URL = "https://relay.a9gent.com/nodes";
const LAUNCHER_BG =
  "radial-gradient(circle at top left, rgba(91, 125, 184, 0.07), transparent 22%), radial-gradient(circle at right 18%, rgba(148, 163, 184, 0.18), transparent 24%), linear-gradient(180deg, #f8fafc 0%, #edf2f7 100%)";
const SURFACE = "var(--mindfs-launcher-surface)";
const SURFACE_STRONG = "var(--mindfs-launcher-surface-strong)";
const BORDER = "var(--mindfs-launcher-border)";
const BORDER_STRONG = "var(--mindfs-launcher-border-strong)";
const TEXT = "var(--mindfs-launcher-text)";
const MUTED = "var(--mindfs-launcher-muted)";
const ACCENT = "var(--mindfs-launcher-accent)";
const SHADOW = "var(--mindfs-launcher-shadow)";

function normalizeNodeURL(value: string): string {
  const trimmed = value.trim();
  if (!trimmed) {
    return "";
  }
  const withScheme = /^[a-z]+:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`;
  try {
    const parsed = new URL(withScheme);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return "";
    }
    parsed.hash = "";
    const relayNodePath = /^\/n\/[^/]+\/?$/.test(parsed.pathname);
    const normalized = parsed.toString().replace(/\/+$/, "");
    return relayNodePath ? `${normalized}/` : normalized;
  } catch {
    return "";
  }
}

function sortNodes(nodes: LauncherNode[]): LauncherNode[] {
  return [...nodes].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
}

function buildNodeID(): string {
  return `node-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

function normalizeLauncherNodes(nodes: LauncherNode[]): LauncherNode[] {
  return nodes
    .map((item) => {
      const id = String(item?.id || "").trim();
      const name = String(item?.name || "").trim();
      const url = normalizeNodeURL(String(item?.url || ""));
      const createdAt = String(item?.createdAt || "").trim();
      const lastOpenedAt = String(item?.lastOpenedAt || "").trim();
      if (!id || !name || !url || !createdAt) {
        return null;
      }
      return {
        id,
        name,
        url,
        createdAt,
        ...(lastOpenedAt ? { lastOpenedAt } : {}),
      };
    })
    .filter((item): item is LauncherNode => item !== null);
}

function mergeLauncherNodes(...groups: LauncherNode[][]): LauncherNode[] {
  const seenURLs = new Set<string>();
  const merged: LauncherNode[] = [];
  for (const group of groups) {
    for (const node of normalizeLauncherNodes(group)) {
      if (seenURLs.has(node.url)) {
        continue;
      }
      seenURLs.add(node.url);
      merged.push(node);
    }
  }
  return sortNodes(merged);
}

function shouldShowAndroidUpdate(state: AndroidUpdateState): boolean {
  const status = (state.status || "idle").toLowerCase();
  return (
    state.has_update === true ||
    status === "downloading" ||
    status === "downloaded" ||
    status === "failed"
  );
}

function androidUpdateSummary(state: AndroidUpdateState): string {
  const notes = String(state.notes || "").trim();
  if (notes) {
    return notes;
  }
  if (state.latest_version) {
    return `发现 Android ${state.latest_version} 新版本`;
  }
  return "";
}

export function Login({ onOpenNode }: LoginProps): ReactElement {
  const [nodes, setNodes] = useState<LauncherNode[]>(() => sortNodes(getStoredLauncherNodes()));
  const [composerOpen, setComposerOpen] = useState(false);
  const [nodeName, setNodeName] = useState("");
  const [nodeURL, setNodeURL] = useState("");
  const [formError, setFormError] = useState("");
  const [editingNodeID, setEditingNodeID] = useState("");
  const [editingNodeName, setEditingNodeName] = useState("");
  const [androidUpdateState, setAndroidUpdateState] =
    useState<AndroidUpdateState>(() => normalizeAndroidUpdateState(null));
  const [androidUpdateNotesOpen, setAndroidUpdateNotesOpen] = useState(false);
  const [androidUpdateBusy, setAndroidUpdateBusy] = useState(false);

  function persistNodes(nextNodes: LauncherNode[]): void {
    const sorted = sortNodes(nextNodes);
    setNodes(sorted);
    setStoredLauncherNodes(sorted);
    void setNativeLauncherNodes(sorted);
  }

  function openNode(node: LauncherNode): void {
    onOpenNode(node.url);
  }

  function handleDeleteNode(nodeID: string): void {
    if (editingNodeID === nodeID) {
      setEditingNodeID("");
      setEditingNodeName("");
    }
    persistNodes(nodes.filter((item) => item.id !== nodeID));
  }

  function handleStartRename(node: LauncherNode): void {
    setEditingNodeID(node.id);
    setEditingNodeName(node.name);
  }

  function handleCancelRename(): void {
    setEditingNodeID("");
    setEditingNodeName("");
  }

  function handleCommitRename(node: LauncherNode): void {
    const trimmedName = editingNodeName.trim();
    if (!trimmedName) {
      setEditingNodeName(node.name);
      setEditingNodeID("");
      return;
    }
    if (trimmedName === node.name) {
      setEditingNodeID("");
      setEditingNodeName("");
      return;
    }
    persistNodes(
      nodes.map((item) =>
        item.id === node.id ? { ...item, name: trimmedName } : item
      )
    );
    setEditingNodeID("");
    setEditingNodeName("");
  }

  function handleSaveNode(event: React.FormEvent): void {
    event.preventDefault();
    const trimmedName = nodeName.trim();
    const normalizedURL = normalizeNodeURL(nodeURL);
    if (!trimmedName) {
      setFormError("Node name is required.");
      return;
    }
    if (!normalizedURL) {
      setFormError("Enter a valid http:// or https:// node URL.");
      return;
    }
    if (nodes.some((item) => item.url === normalizedURL)) {
      setFormError("This node URL already exists.");
      return;
    }
    const createdAt = new Date().toISOString();
    const nextNode: LauncherNode = {
      id: buildNodeID(),
      name: trimmedName,
      url: normalizedURL,
      createdAt,
    };
    persistNodes([nextNode, ...nodes]);
    setNodeName("");
    setNodeURL("");
    setFormError("");
    setComposerOpen(false);
    onOpenNode(nextNode.url);
  }

  async function handleDownloadAndroidUpdate(): Promise<void> {
    const next = normalizeAndroidUpdateState(androidUpdateState);
    if (!next.download_url || androidUpdateBusy) {
      return;
    }
    setAndroidUpdateBusy(true);
    setAndroidUpdateState((prev) =>
      normalizeAndroidUpdateState({
        ...prev,
        status: "downloading",
        message: "正在下载 Android 更新包",
      }),
    );
    try {
      await downloadURL(next.download_url, next.filename || "mindfs-android.apk");
      setAndroidUpdateState((prev) =>
        normalizeAndroidUpdateState({
          ...prev,
          status: "downloaded",
          message: "更新包已开始下载，请在系统通知或下载目录中打开安装",
        }),
      );
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Android 更新下载失败";
      setAndroidUpdateState((prev) =>
        normalizeAndroidUpdateState({
          ...prev,
          status: "failed",
          message,
        }),
      );
    } finally {
      setAndroidUpdateBusy(false);
    }
  }

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const [nativeNodes, pendingNodes] = await Promise.all([
        getNativeLauncherNodes(),
        consumePendingRelayNodes(),
      ]);
      if (cancelled) {
        return;
      }

      const existingNodes = getStoredLauncherNodes();
      const restoredNodes = mergeLauncherNodes(existingNodes, nativeNodes);
      const existingURLSet = new Set(
        restoredNodes.map((item) => normalizeNodeURL(item.url)).filter(Boolean),
      );
      const createdAt = new Date().toISOString();
      const importedNodes: LauncherNode[] = [];

      for (const item of pendingNodes) {
        const name = String(item?.name || "").trim();
        const url = normalizeNodeURL(String(item?.url || ""));
        if (!name || !url || existingURLSet.has(url)) {
          continue;
        }
        existingURLSet.add(url);
        importedNodes.push({
          id: buildNodeID(),
          name,
          url,
          createdAt,
        });
      }

      const nextNodes = mergeLauncherNodes(importedNodes, restoredNodes);
      if (
        nextNodes.length === existingNodes.length &&
        nextNodes.length === nativeNodes.length &&
        importedNodes.length === 0
      ) {
        return;
      }

      setStoredLauncherNodes(nextNodes);
      void setNativeLauncherNodes(nextNodes);
      if (!cancelled) {
        setNodes(nextNodes);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!isAndroidRuntime()) {
      setAndroidUpdateState(normalizeAndroidUpdateState(null));
      return;
    }

    let cancelled = false;
    void fetchAndroidUpdateState()
      .then((state) => {
        if (!cancelled) {
          setAndroidUpdateState(state);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          console.warn("[android-update] launcher check failed", error);
          setAndroidUpdateState(normalizeAndroidUpdateState(null));
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const showAndroidUpdate = shouldShowAndroidUpdate(androidUpdateState);
  const androidUpdateStatus = (androidUpdateState.status || "idle").toLowerCase();
  const androidUpdateDisabled =
    androidUpdateBusy ||
    androidUpdateStatus === "downloading" ||
    androidUpdateStatus === "downloaded";
  const androidUpdateText =
    androidUpdateStatus === "downloading"
      ? "下载中..."
      : androidUpdateStatus === "downloaded"
        ? "已开始下载"
        : "更新APP";
  const androidUpdateHelp =
    androidUpdateState.message ||
    (androidUpdateState.latest_version
      ? `当前 ${androidUpdateState.current_version || "未知"}，最新 ${androidUpdateState.latest_version}`
      : "");
  const androidUpdateNotes = androidUpdateSummary(androidUpdateState);

  return (
    <div
      style={{
        minHeight: "100dvh",
        background: "var(--mindfs-system-bar-bg)",
        color: TEXT,
        padding:
          "calc(var(--mindfs-safe-area-top, env(safe-area-inset-top, 0px)) + 20px) 16px calc(var(--mindfs-safe-area-bottom, env(safe-area-inset-bottom, 0px)) + 24px)",
        display: "flex",
        justifyContent: "center",
      }}
    >
      <div
        style={{
          position: "fixed",
          inset: 0,
          background: `var(--mindfs-launcher-bg, ${LAUNCHER_BG})`,
          pointerEvents: "none",
          zIndex: 0,
        }}
      />
      <div
        style={{
          position: "relative",
          zIndex: 1,
          width: "100%",
          maxWidth: "640px",
          minHeight:
            "calc(100dvh - var(--mindfs-safe-area-top, env(safe-area-inset-top, 0px)) - var(--mindfs-safe-area-bottom, env(safe-area-inset-bottom, 0px)) - 44px)",
          display: "flex",
          flexDirection: "column",
          gap: "8px",
        }}
      >
        <button
          type="button"
          onClick={() => onOpenNode(RELAY_URL)}
          style={{
            width: "100%",
            textAlign: "left",
            border: `1px solid ${BORDER}`,
            borderRadius: "20px",
            background: SURFACE_STRONG,
            padding: "18px",
            fontSize: "18px",
            fontWeight: 500,
            color: TEXT,
            cursor: "pointer",
            boxShadow: SHADOW,
            backdropFilter: "blur(20px)",
          }}
        >
          <div style={{ minWidth: 0, display: "grid", gap: "4px" }}>
            <div
              style={{
                fontSize: "18px",
                fontWeight: 500,
                color: TEXT,
                lineHeight: 1.2,
              }}
            >
              mindfs relayer
            </div>
            <div
              style={{
                fontSize: "12px",
                lineHeight: 1.5,
                color: MUTED,
                wordBreak: "break-word",
              }}
            >
              {RELAY_URL}
            </div>
          </div>
        </button>

        {nodes.map((node) => (
          <div
            key={node.id}
            style={{
              width: "100%",
              borderRadius: "20px",
              border: `1px solid ${BORDER}`,
              background: SURFACE,
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
              gap: "16px",
              boxShadow: SHADOW,
              backdropFilter: "blur(20px)",
            }}
          >
            <div
              onClick={() => {
                if (editingNodeID !== node.id) {
                  openNode(node);
                }
              }}
              style={{
                flex: 1,
                minWidth: 0,
                textAlign: "left",
                padding: "16px 0 16px 18px",
                cursor: editingNodeID === node.id ? "default" : "pointer",
              }}
            >
              <div style={{ minWidth: 0, display: "grid", gap: "4px" }}>
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "8px",
                    minWidth: 0,
                  }}
                >
                  {editingNodeID === node.id ? (
                    <input
                      type="text"
                      value={editingNodeName}
                      autoFocus
                      onClick={(event) => event.stopPropagation()}
                      onChange={(event) => setEditingNodeName(event.target.value)}
                      onBlur={() => handleCommitRename(node)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          handleCommitRename(node);
                        }
                        if (event.key === "Escape") {
                          event.preventDefault();
                          handleCancelRename();
                        }
                      }}
                      style={{
                        flex: 1,
                        minWidth: 0,
                        borderRadius: "10px",
                        border: `1px solid ${BORDER_STRONG}`,
                        background: "var(--mindfs-launcher-input-bg)",
                        padding: "6px 10px",
                        fontSize: "17px",
                        fontWeight: 500,
                        color: TEXT,
                        lineHeight: 1.2,
                        outline: "none",
                      }}
                    />
                  ) : (
                    <>
                      <div
                        style={{
                          fontSize: "17px",
                          fontWeight: 500,
                          color: TEXT,
                          lineHeight: 1.2,
                          wordBreak: "break-word",
                        }}
                      >
                        {node.name}
                      </div>
                      <button
                        type="button"
                        aria-label={`重命名 ${node.name}`}
                        onClick={(event) => {
                          event.stopPropagation();
                          handleStartRename(node);
                        }}
                        style={{
                          border: "none",
                          background: "transparent",
                          color: MUTED,
                          display: "inline-flex",
                          alignItems: "center",
                          justifyContent: "center",
                          width: "22px",
                          height: "22px",
                          padding: 0,
                          cursor: "pointer",
                          flex: "0 0 auto",
                        }}
                      >
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                          <path d="M12 20h9" />
                          <path d="M16.5 3.5a2.12 2.12 0 1 1 3 3L7 19l-4 1 1-4Z" />
                        </svg>
                      </button>
                    </>
                  )}
                </div>
                <div
                  style={{
                    fontSize: "12px",
                    lineHeight: 1.5,
                    color: MUTED,
                    wordBreak: "break-word",
                  }}
                >
                  {node.url}
                </div>
              </div>
            </div>
            <button
              type="button"
              aria-label={`删除 ${node.name}`}
              onClick={(event) => {
                event.stopPropagation();
                handleDeleteNode(node.id);
              }}
              style={{
                border: "none",
                background: "transparent",
                color: ACCENT,
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                width: "48px",
                height: "100%",
                minHeight: "72px",
                padding: "0 14px 0 0",
                cursor: "pointer",
                flex: "0 0 auto",
              }}
            >
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M3 6h18" />
                <path d="M8 6V4h8v2" />
                <path d="M19 6l-1 14H6L5 6" />
                <path d="M10 11v6" />
                <path d="M14 11v6" />
              </svg>
            </button>
          </div>
        ))}

        {showAndroidUpdate ? (
          <div
            style={{
              border: `1px solid ${BORDER}`,
              borderRadius: "20px",
              background: SURFACE_STRONG,
              padding: "14px",
              boxShadow: SHADOW,
              backdropFilter: "blur(20px)",
              display: "grid",
              gap: "10px",
              marginTop: "auto",
            }}
          >
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: "12px",
              }}
            >
              <div style={{ minWidth: 0, display: "grid", gap: "3px" }}>
                <div
                  style={{
                    fontSize: "15px",
                    fontWeight: 600,
                    color: TEXT,
                    lineHeight: 1.25,
                  }}
                >
                  新版本
                </div>
                {androidUpdateHelp ? (
                  <div
                    style={{
                      fontSize: "12px",
                      color: MUTED,
                      lineHeight: 1.45,
                      wordBreak: "break-word",
                    }}
                  >
                    {androidUpdateHelp}
                  </div>
                ) : null}
              </div>
              <button
                type="button"
                disabled={androidUpdateDisabled}
                onClick={() => {
                  void handleDownloadAndroidUpdate();
                }}
                style={{
                  border: "none",
                  borderRadius: "14px",
                  background: androidUpdateDisabled ? "rgba(148, 163, 184, 0.35)" : ACCENT,
                  color: androidUpdateDisabled ? MUTED : "#fff8f2",
                  padding: "10px 14px",
                  fontSize: "13px",
                  fontWeight: 600,
                  cursor: androidUpdateDisabled ? "not-allowed" : "pointer",
                  flexShrink: 0,
                  minWidth: "86px",
                }}
              >
                {androidUpdateText}
              </button>
            </div>
            {androidUpdateNotes ? (
              <>
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "8px",
                    flexWrap: "wrap",
                  }}
                >
                  <button
                    type="button"
                    onClick={() => setAndroidUpdateNotesOpen((open) => !open)}
                    style={{
                      border: "none",
                      background: "transparent",
                      color: ACCENT,
                      padding: 0,
                      justifySelf: "start",
                      fontSize: "12px",
                      fontWeight: 600,
                      cursor: "pointer",
                    }}
                  >
                    {androidUpdateNotesOpen ? "收起更新说明" : "查看更新说明"}
                  </button>
                  <span
                    style={{
                      color: "#dc2626",
                      fontSize: "12px",
                      fontWeight: 700,
                      lineHeight: 1.35,
                    }}
                  >
                    请先将 mindfs 后端升级到最新版本
                  </span>
                </div>
                {androidUpdateNotesOpen ? (
                  <div
                    style={{
                      borderTop: `1px solid ${BORDER}`,
                      paddingTop: "10px",
                      color: MUTED,
                      fontSize: "12px",
                      lineHeight: 1.55,
                      whiteSpace: "pre-wrap",
                      wordBreak: "break-word",
                      maxHeight: "180px",
                      overflow: "auto",
                    }}
                  >
                    {androidUpdateNotes}
                  </div>
                ) : null}
              </>
            ) : null}
          </div>
        ) : null}

        <button
          type="button"
          onClick={() => {
            setComposerOpen(true);
            setFormError("");
          }}
          style={{
            width: "100%",
            textAlign: "center",
            border: `1px dashed ${BORDER_STRONG}`,
            borderRadius: "20px",
            background: "var(--mindfs-launcher-surface-soft)",
            padding: "18px",
            fontSize: "24px",
            fontWeight: 500,
            color: MUTED,
            cursor: "pointer",
            boxShadow: SHADOW,
            backdropFilter: "blur(20px)",
            marginTop: showAndroidUpdate ? 0 : "auto",
          }}
        >
          +
        </button>
      </div>

      {composerOpen ? (
        <div
          style={{
            position: "fixed",
            inset: 0,
            background: "rgba(17, 24, 39, 0.18)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            padding: "20px",
            zIndex: 40,
          }}
        >
          <form
            onSubmit={handleSaveNode}
            style={{
              width: "100%",
              maxWidth: "420px",
              borderRadius: "22px",
              background: SURFACE_STRONG,
              border: `1px solid var(--mindfs-launcher-modal-border)`,
              boxShadow: SHADOW,
              padding: "18px",
              backdropFilter: "blur(20px)",
              display: "grid",
              gap: "12px",
            }}
          >
            <input
              type="text"
              value={nodeName}
              onChange={(event) => setNodeName(event.target.value)}
              placeholder="节点名称"
              autoFocus
              style={{
                width: "100%",
                borderRadius: "14px",
                border: `1px solid ${formError ? "var(--mindfs-launcher-error-text)" : BORDER_STRONG}`,
                background: "var(--mindfs-launcher-input-bg)",
                padding: "14px 15px",
                fontSize: "15px",
                color: TEXT,
                outline: "none",
                boxSizing: "border-box",
              }}
            />

            <input
              type="text"
              value={nodeURL}
              onChange={(event) => setNodeURL(event.target.value)}
              placeholder="节点 url：http(s)://ip:port"
              spellCheck={false}
              style={{
                width: "100%",
                borderRadius: "14px",
                border: `1px solid ${formError ? "var(--mindfs-launcher-error-text)" : BORDER_STRONG}`,
                background: "var(--mindfs-launcher-input-bg)",
                padding: "14px 15px",
                fontSize: "15px",
                color: TEXT,
                outline: "none",
                boxSizing: "border-box",
              }}
            />

            <div
              style={{
                display: "grid",
                gridTemplateColumns: "1fr 1fr",
                gap: "10px",
              }}
            >
              <button
                type="button"
                onClick={() => {
                  setComposerOpen(false);
                  setFormError("");
                }}
                style={{
                  border: `1px solid ${BORDER_STRONG}`,
                  borderRadius: "14px",
                  padding: "12px 16px",
                  background: "transparent",
                  color: MUTED,
                  fontSize: "14px",
                  fontWeight: 500,
                  cursor: "pointer",
                  width: "100%",
                }}
              >
                取消
              </button>
              <button
                type="submit"
                style={{
                  border: "none",
                  borderRadius: "14px",
                  padding: "12px 16px",
                  background: ACCENT,
                  color: "#fff8f2",
                  fontSize: "14px",
                  fontWeight: 500,
                  cursor: "pointer",
                  width: "100%",
                }}
              >
                保存
              </button>
            </div>
          </form>
        </div>
      ) : null}
    </div>
  );
}
