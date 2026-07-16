import React, { useEffect, useState, type ReactElement } from "react";
import {
  Alert,
  Button,
  Card,
  Input,
  Modal,
  Popconfirm,
  Space,
  Typography,
} from "antd";
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
} from "@ant-design/icons";
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
  appPackageLabel,
  fetchAppUpdateState,
  isUpdatableNativeRuntime,
  normalizeAppUpdateState,
  type AppUpdateState,
} from "../services/appUpdate";
import { downloadURL } from "../services/download";
import { useI18n, type MessageKey, type MessageParams } from "../i18n";

type LoginProps = {
  onOpenNode: (nodeURL: string) => void;
};

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
const { Text, Title } = Typography;

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

function shouldShowAppUpdate(state: AppUpdateState): boolean {
  const status = (state.status || "idle").toLowerCase();
  return (
    state.has_update === true ||
    status === "downloading" ||
    status === "downloaded" ||
    status === "failed"
  );
}

function appUpdateSummary(state: AppUpdateState, t: (key: MessageKey, params?: MessageParams) => string): string {
  const notes = String(state.notes || "").trim();
  if (notes) {
    return notes;
  }
  if (state.latest_version) {
    return t("login.updateAvailable", {
      packageLabel: appPackageLabel(state.platform),
      version: state.latest_version,
    });
  }
  return "";
}

export function Login({ onOpenNode }: LoginProps): ReactElement {
  const { t } = useI18n();
  const [nodes, setNodes] = useState<LauncherNode[]>(() => sortNodes(getStoredLauncherNodes()));
  const [composerOpen, setComposerOpen] = useState(false);
  const [nodeName, setNodeName] = useState("");
  const [nodeURL, setNodeURL] = useState("");
  const [formError, setFormError] = useState("");
  const [editingNodeID, setEditingNodeID] = useState("");
  const [editingNodeName, setEditingNodeName] = useState("");
  const [appUpdateState, setAppUpdateState] =
    useState<AppUpdateState>(() => normalizeAppUpdateState(null));
  const [appUpdateNotesOpen, setAppUpdateNotesOpen] = useState(false);
  const [appUpdateBusy, setAppUpdateBusy] = useState(false);

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
  }

  async function handleDownloadAppUpdate(): Promise<void> {
    const next = normalizeAppUpdateState(appUpdateState);
    const packageLabel = appPackageLabel(next.platform);
    if (!next.download_url || appUpdateBusy) {
      return;
    }
    setAppUpdateBusy(true);
    setAppUpdateState((prev) =>
      normalizeAppUpdateState({
        ...prev,
        status: "downloading",
        message: t("login.downloadingPackage", { packageLabel }),
      }),
    );
    try {
      await downloadURL(next.download_url, next.filename || "");
      setAppUpdateState((prev) =>
        normalizeAppUpdateState({
          ...prev,
          status: "downloaded",
          message: t("login.packageDownloadStarted"),
        }),
      );
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t("login.packageDownloadFailed", { packageLabel });
      setAppUpdateState((prev) =>
        normalizeAppUpdateState({
          ...prev,
          status: "failed",
          message,
        }),
      );
    } finally {
      setAppUpdateBusy(false);
    }
  }

  useEffect(() => {
    let cancelled = false;
    const timers: number[] = [];

    const syncLauncherNodes = async (): Promise<void> => {
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
      if (nextNodes.length === existingNodes.length && importedNodes.length === 0) {
        return;
      }

      setStoredLauncherNodes(nextNodes);
      void setNativeLauncherNodes(nextNodes);
      if (!cancelled) {
        setNodes(nextNodes);
      }
    };

    const handleLauncherNodesUpdated = (): void => {
      void syncLauncherNodes();
    };

    void syncLauncherNodes();
    timers.push(window.setTimeout(() => void syncLauncherNodes(), 1200));
    timers.push(window.setTimeout(() => void syncLauncherNodes(), 3200));
    window.addEventListener("mindfs:launcher-nodes-updated", handleLauncherNodesUpdated);

    return () => {
      cancelled = true;
      window.removeEventListener("mindfs:launcher-nodes-updated", handleLauncherNodesUpdated);
      timers.forEach((timer) => window.clearTimeout(timer));
    };
  }, []);

  useEffect(() => {
    if (!isUpdatableNativeRuntime() || nodes.length === 0) {
      setAppUpdateState(normalizeAppUpdateState(null));
      return;
    }

    let cancelled = false;
    void fetchAppUpdateState()
      .then((state) => {
        if (!cancelled) {
          setAppUpdateState(state);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          console.warn("[app-update] launcher check failed", error);
          setAppUpdateState(normalizeAppUpdateState(null));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [nodes.length]);

  const showAppUpdate = shouldShowAppUpdate(appUpdateState);
  const appUpdateStatus = (appUpdateState.status || "idle").toLowerCase();
  const appUpdateDisabled =
    appUpdateBusy ||
    appUpdateStatus === "downloading" ||
    appUpdateStatus === "downloaded";
  const appUpdateText =
    appUpdateStatus === "downloading"
      ? t("login.updateDownloading")
      : appUpdateStatus === "downloaded"
        ? t("login.updateDownloaded")
        : t("login.updateApp");
  const appUpdateHelp =
    appUpdateState.message ||
    (appUpdateState.latest_version
      ? t("login.updateVersionHelp", {
          current: appUpdateState.current_version || t("login.unknownVersion"),
          latest: appUpdateState.latest_version,
        })
      : "");
  const appUpdateNotes = appUpdateSummary(appUpdateState, t);

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
        <Card
          style={{
            width: "100%",
            border: `1px solid ${BORDER}`,
            background: SURFACE_STRONG,
            boxShadow: SHADOW,
            backdropFilter: "blur(20px)",
          }}
          styles={{ body: { padding: 18 } }}
        >
          <div style={{ minWidth: 0, display: "grid", gap: "4px" }}>
            <Title level={4} style={{ margin: 0, color: TEXT, lineHeight: 1.2 }}>
              本地直连
            </Title>
            <Text
              style={{
                fontSize: "12px",
                lineHeight: 1.5,
                color: MUTED,
                wordBreak: "break-word",
              }}
            >
              不再内置中转，请添加你自己的 MindFS 节点地址。
            </Text>
          </div>
        </Card>

        {nodes.map((node) => (
          <Card
            hoverable={editingNodeID !== node.id}
            key={node.id}
            onClick={() => {
              if (editingNodeID !== node.id) {
                openNode(node);
              }
            }}
            style={{
              width: "100%",
              border: `1px solid ${BORDER}`,
              background: SURFACE,
              boxShadow: SHADOW,
              backdropFilter: "blur(20px)",
              cursor: editingNodeID === node.id ? "default" : "pointer",
            }}
            styles={{ body: { padding: "14px 14px 14px 18px" } }}
          >
            <div
              style={{
                display: "grid",
                gridTemplateColumns: "minmax(0, 1fr) auto",
                alignItems: "center",
                gap: "16px",
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
                    <Input
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
                        background: "var(--mindfs-launcher-input-bg)",
                        fontSize: "17px",
                        fontWeight: 500,
                        color: TEXT,
                      }}
                    />
                  ) : (
                    <>
                      <Text
                        strong
                        style={{
                          fontSize: "17px",
                          color: TEXT,
                          lineHeight: 1.2,
                          wordBreak: "break-word",
                        }}
                      >
                        {node.name}
                      </Text>
                      <Button
                        type="text"
                        size="small"
                        icon={<EditOutlined />}
                        aria-label={`重命名 ${node.name}`}
                        onClick={(event) => {
                          event.stopPropagation();
                          handleStartRename(node);
                        }}
                        style={{
                          color: MUTED,
                          flex: "0 0 auto",
                        }}
                      />
                    </>
                  )}
                </div>
                <Text
                  style={{
                    fontSize: "12px",
                    lineHeight: 1.5,
                    color: MUTED,
                    wordBreak: "break-word",
                  }}
                >
                  {node.url}
                </Text>
              </div>
              <Popconfirm
                title="删除节点"
                description={`确认删除 ${node.name}？`}
                okText="删除"
                cancelText="取消"
                okButtonProps={{ danger: true }}
                onConfirm={(event) => {
                  event?.stopPropagation();
                  handleDeleteNode(node.id);
                }}
                onCancel={(event) => event?.stopPropagation()}
              >
                <Button
                  type="text"
                  danger
                  icon={<DeleteOutlined />}
                  aria-label={`删除 ${node.name}`}
                  onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                  }}
                  style={{ flex: "0 0 auto" }}
                />
              </Popconfirm>
            </div>
          </Card>
        ))}

        {showAppUpdate ? (
          <Card
            style={{
              border: `1px solid ${BORDER}`,
              background: SURFACE_STRONG,
              boxShadow: SHADOW,
              backdropFilter: "blur(20px)",
              marginTop: "auto",
            }}
            styles={{ body: { padding: 14, display: "grid", gap: 10 } }}
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
                <Text
                  strong
                  style={{
                    fontSize: "15px",
                    color: TEXT,
                    lineHeight: 1.25,
                  }}
                >
                  新版本
                </Text>
                {appUpdateHelp ? (
                  <Text
                    style={{
                      fontSize: "12px",
                      color: MUTED,
                      lineHeight: 1.45,
                      wordBreak: "break-word",
                    }}
                  >
                    {appUpdateHelp}
                  </Text>
                ) : null}
              </div>
              <Button
                type="primary"
                disabled={appUpdateDisabled}
                loading={appUpdateBusy || appUpdateStatus === "downloading"}
                onClick={() => {
                  void handleDownloadAppUpdate();
                }}
                style={{
                  cursor: appUpdateDisabled ? "not-allowed" : "pointer",
                  flexShrink: 0,
                  minWidth: "86px",
                }}
              >
                {appUpdateText}
              </Button>
            </div>
            {appUpdateNotes ? (
              <>
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "8px",
                    flexWrap: "wrap",
                  }}
                >
                  <Button
                    type="link"
                    size="small"
                    onClick={() => setAppUpdateNotesOpen((open) => !open)}
                    style={{
                      color: ACCENT,
                      padding: 0,
                      justifySelf: "start",
                    }}
                  >
                    {appUpdateNotesOpen ? "收起更新说明" : "查看更新说明"}
                  </Button>
                  <Alert
                    type="warning"
                    showIcon
                    message="请先将 mindfs 后端升级到最新版本"
                    style={{ padding: "4px 8px", fontSize: 12 }}
                  />
                </div>
                {appUpdateNotesOpen ? (
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
                    {appUpdateNotes}
                  </div>
                ) : null}
              </>
            ) : null}
          </Card>
        ) : null}

        <Button
          block
          icon={<PlusOutlined />}
          aria-label="新增节点"
          onClick={() => {
            setComposerOpen(true);
            setFormError("");
          }}
          style={{
            width: "100%",
            textAlign: "center",
            border: `1px dashed ${BORDER_STRONG}`,
            background: "var(--mindfs-launcher-surface-soft)",
            height: "60px",
            fontSize: "18px",
            color: MUTED,
            boxShadow: SHADOW,
            backdropFilter: "blur(20px)",
            marginTop: showAppUpdate ? 0 : "auto",
          }}
        >
          新增节点
        </Button>
      </div>

      <Modal
        open={composerOpen}
        title="新增节点"
        centered
        footer={null}
        onCancel={() => {
          setComposerOpen(false);
          setFormError("");
        }}
        className="mindfs-launcher-modal"
      >
        <form
          onSubmit={handleSaveNode}
          style={{
            width: "100%",
            display: "grid",
            gap: "12px",
          }}
        >
          <Input
            type="text"
            value={nodeName}
            onChange={(event) => setNodeName(event.target.value)}
            placeholder="节点名称"
            autoFocus
            style={{
              background: "var(--mindfs-launcher-input-bg)",
              fontSize: "15px",
              color: TEXT,
            }}
            status={formError ? "error" : undefined}
          />

          <Input
            type="text"
            value={nodeURL}
            onChange={(event) => setNodeURL(event.target.value)}
            placeholder="节点 url：http(s)://ip:port"
            spellCheck={false}
            style={{
              background: "var(--mindfs-launcher-input-bg)",
              fontSize: "15px",
              color: TEXT,
            }}
            status={formError ? "error" : undefined}
          />

          {formError ? <Alert type="error" showIcon message={formError} /> : null}

          <Space.Compact block>
            <Button
              onClick={() => {
                setComposerOpen(false);
                setFormError("");
              }}
              style={{ width: "50%" }}
            >
              取消
            </Button>
            <Button
              type="primary"
              htmlType="submit"
              style={{ width: "50%" }}
            >
              保存
            </Button>
          </Space.Compact>
        </form>
      </Modal>
    </div>
  );
}
