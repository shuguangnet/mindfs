import React, { useEffect, useMemo, useRef, useState } from "react";
import type { AgentStatus } from "../services/agents";
import type { AgentAPIProvider } from "../services/agentConfig";
import {
  buildModelProviderIndex,
  filterAgentModels,
  resolveModelGroups,
} from "./modelFiltering";

type ModelSelectorProps = {
  agent?: AgentStatus | null;
  model?: string;
  onModelChange: (model: string) => void;
  compact?: boolean;
  menuPlacement?: "top" | "bottom";
  maxButtonWidth?: string;
  /** 供应商目录，用于按供应商分组展示。 */
  providers?: AgentAPIProvider[] | null;
};

const MODEL_SEARCH_THRESHOLD = 8;

/** 模型选择器：独立选择模型，不包含运行参数（模式/思考等级/Fast）。 */
export function ModelSelector({
  agent,
  model = "",
  onModelChange,
  compact = false,
  menuPlacement = "top",
  maxButtonWidth = "min(30vw, 132px)",
  providers = null,
}: ModelSelectorProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const models = agent?.models ?? [];
  const selectedModel = useMemo(() => {
    const fallback = agent?.default_model_id || agent?.current_model_id || "";
    const target = model || fallback;
    return models.find((item) => item.id === target) ?? null;
  }, [agent, model, models]);
  const displayName = selectedModel?.name || selectedModel?.id || model || "模型";
  const hasModelContent = models.length > 0;

  useEffect(() => {
    if (!isOpen) return;
    const handlePointerOutside = (event: PointerEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener("pointerdown", handlePointerOutside);
    return () => document.removeEventListener("pointerdown", handlePointerOutside);
  }, [isOpen]);

  useEffect(() => setIsOpen(false), [agent?.name]);

  const closeMenu = () => setIsOpen(false);

  const enableSearch = hasModelContent && models.length > MODEL_SEARCH_THRESHOLD;
  useEffect(() => {
    if (!isOpen) {
      setQuery("");
      return;
    }
    if (enableSearch) {
      // 等待菜单渲染完成后聚焦搜索框。
      const timer = window.setTimeout(() => searchRef.current?.focus(), 16);
      return () => window.clearTimeout(timer);
    }
  }, [isOpen, enableSearch]);

  const providerIndex = useMemo(() => buildModelProviderIndex(providers), [providers]);
  const filteredModels = useMemo(() => filterAgentModels(models, query), [models, query]);
  const modelGroups = useMemo(
    () => (enableSearch ? resolveModelGroups(filteredModels, providerIndex) : null),
    [enableSearch, filteredModels, providerIndex],
  );

  if (!agent && !model) return null;

  const renderModelButton = (item: (typeof models)[number]) => (
    <button
      key={item.id}
      type="button"
      onClick={() => {
        onModelChange(item.id);
        closeMenu();
      }}
      title={item.description || item.id}
      style={sectionItemStyle(item.id === selectedModel?.id, item.hidden ? 0.66 : 1)}
    >
      <span style={{ fontSize: "13px", fontWeight: 600 }}>{item.name || item.id}</span>
      {item.description ? <span style={descriptionStyle}>{item.description}</span> : null}
    </button>
  );

  return (
    <div ref={dropdownRef} style={{ position: "relative", minWidth: 0 }}>
      <button
        type="button"
        disabled={!hasModelContent || !agent?.available}
        onClick={() => setIsOpen((previous) => !previous)}
        title={selectedModel?.description || selectedModel?.id || model || "当前 Agent 未提供模型"}
        aria-label={`选择模型，当前为 ${displayName}`}
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: "4px",
          maxWidth: maxButtonWidth,
          height: compact ? "28px" : "32px",
          padding: compact ? "0 5px" : "0 8px",
          border: "none",
          borderRadius: "10px",
          background: isOpen ? "rgba(59,130,246,0.08)" : "transparent",
          color: agent?.available === false ? "var(--text-secondary)" : "var(--text-primary)",
          cursor: hasModelContent && agent?.available !== false ? "pointer" : "default",
          opacity: hasModelContent ? 1 : 0.58,
          outline: "none",
        }}
      >
        <span
          style={{
            minWidth: 0,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
            fontSize: "12px",
            fontWeight: 600,
          }}
        >
          {displayName}
        </span>
        <SelectorChevron expanded={isOpen} />
      </button>

      {isOpen ? (
        <div
          style={{
            position: "absolute",
            ...(menuPlacement === "bottom"
              ? { top: "calc(100% + 8px)" }
              : { bottom: "calc(100% + 8px)" }),
            right: 0,
            width: "min(280px, calc(100vw - 16px))",
            maxHeight: "360px",
            overflowY: "auto",
            padding: "8px 0",
            border: "1px solid var(--menu-border)",
            borderRadius: "12px",
            background: "var(--menu-bg)",
            boxShadow: "0 8px 32px rgba(0,0,0,0.15)",
            zIndex: 1000,
          }}
        >
          {enableSearch ? (
            <div style={{ position: "sticky", top: 0, zIndex: 1, padding: "0 10px 8px", background: "var(--menu-bg)" }}>
              <input
                ref={searchRef}
                type="text"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="搜索模型..."
                style={{
                  width: "100%",
                  boxSizing: "border-box",
                  height: "30px",
                  padding: "0 10px",
                  border: "1px solid var(--border-color)",
                  borderRadius: "8px",
                  background: "transparent",
                  color: "var(--text-primary)",
                  fontSize: "12px",
                  outline: "none",
                }}
              />
            </div>
          ) : null}
          {filteredModels.length === 0 ? (
            <div style={{ padding: "10px 12px", fontSize: "12px", color: "var(--text-secondary)" }}>
              未找到匹配的模型
            </div>
          ) : modelGroups ? (
            modelGroups.map((group) => (
              <div key={group.provider || "__other__"}>
                <div
                  style={{
                    padding: "6px 12px 4px",
                    fontSize: "11px",
                    fontWeight: 600,
                    color: "var(--text-secondary)",
                    letterSpacing: "0.02em",
                  }}
                >
                  {group.provider || "其他"}
                </div>
                {group.models.map(renderModelButton)}
              </div>
            ))
          ) : (
            filteredModels.map(renderModelButton)
          )}
        </div>
      ) : null}
    </div>
  );
}

const descriptionStyle: React.CSSProperties = {
  fontSize: "11px",
  color: "var(--text-secondary)",
  whiteSpace: "normal",
  overflowWrap: "anywhere",
  wordBreak: "break-word",
};

function SelectorChevron({ expanded }: { expanded: boolean }) {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 12 12"
      fill="none"
      aria-hidden="true"
      style={{
        flexShrink: 0,
        color: expanded ? "#3b82f6" : "var(--text-secondary)",
        transform: expanded ? "rotate(180deg)" : "none",
        transition: "transform 0.16s ease",
      }}
    >
      <path d="m2.5 4 3.5 4 3.5-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function sectionItemStyle(selected: boolean, opacity = 1): React.CSSProperties {
  return {
    display: "flex",
    flexDirection: "column",
    alignItems: "flex-start",
    gap: "2px",
    width: "100%",
    minWidth: 0,
    padding: "10px 12px",
    border: "none",
    background: selected ? "rgba(59,130,246,0.08)" : "transparent",
    color: selected ? "#3b82f6" : "var(--text-primary)",
    textAlign: "left",
    cursor: "pointer",
    opacity,
  };
}