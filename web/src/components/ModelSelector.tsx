import React, { useEffect, useMemo, useRef, useState } from "react";
import type { AgentStatus } from "../services/agents";

type ModelSelectorProps = {
  agent?: AgentStatus | null;
  model?: string;
  mode?: string;
  effort?: string;
  fastService?: "" | "on" | "off";
  onModelChange: (model: string) => void;
  onModeChange?: (mode?: string) => void;
  onEffortChange?: (effort?: string) => void;
  onFastServiceChange?: (fastService?: "" | "on" | "off") => void;
  compact?: boolean;
  menuPlacement?: "top" | "bottom";
  maxButtonWidth?: string;
};

/** 模型及其相关运行参数独立于 Agent 选择，避免一个入口同时改变两类状态。 */
export function ModelSelector({
  agent,
  model = "",
  mode = "",
  effort = "",
  fastService = "",
  onModelChange,
  onModeChange,
  onEffortChange,
  onFastServiceChange,
  compact = false,
  menuPlacement = "top",
  maxButtonWidth = "min(30vw, 132px)",
}: ModelSelectorProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [modelExpanded, setModelExpanded] = useState(true);
  const [modeExpanded, setModeExpanded] = useState(false);
  const [effortExpanded, setEffortExpanded] = useState(false);
  const [serviceTierExpanded, setServiceTierExpanded] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const models = agent?.models ?? [];
  const selectedModel = useMemo(() => {
    const fallback = agent?.default_model_id || agent?.current_model_id || "";
    const target = model || fallback;
    return models.find((item) => item.id === target) ?? null;
  }, [agent, model, models]);
  const modelEfforts = selectedModel?.efforts ?? [];
  const efforts = modelEfforts.length > 0 ? modelEfforts : agent?.efforts ?? [];
  const modes = agent?.modes ?? [];
  const supportsEffort = efforts.length > 0 && !!selectedModel?.supportEffort;
  const supportsFastService = !!agent?.supports_fast_service;
  const displayedMode = mode || agent?.current_mode_id || "";
  const displayedEffort = effort || selectedModel?.default_effort || agent?.default_effort || "Auto";
  const fastModeEnabled = (fastService || agent?.default_fast_service || "") === "on";
  const hasMenuContent = models.length > 0 || modes.length > 0 || supportsEffort || supportsFastService;
  const displayName = selectedModel?.name || selectedModel?.id || model || "模型";

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

  if (!agent && !model) return null;

  return (
    <div ref={dropdownRef} style={{ position: "relative", minWidth: 0 }}>
      <button
        type="button"
        disabled={!hasMenuContent || !agent?.available}
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
          cursor: hasMenuContent && agent?.available !== false ? "pointer" : "default",
          opacity: hasMenuContent ? 1 : 0.58,
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
          {models.length > 0 ? (
            <>
              <SectionHeader title="模型" expanded={modelExpanded} onToggle={() => setModelExpanded((value) => !value)} value={selectedModel?.id} />
              {modelExpanded
                ? models.map((item, index) => (
                    <button
                      key={item.id}
                      type="button"
                      onClick={() => {
                        onModelChange(item.id);
                        closeMenu();
                      }}
                      title={item.description || item.id}
                      style={sectionItemStyle(item.id === selectedModel?.id, index > 0, item.hidden ? 0.66 : 1)}
                    >
                      <span style={{ fontSize: "13px", fontWeight: 600 }}>{item.name || item.id}</span>
                      {item.description ? <span style={descriptionStyle}>{item.description}</span> : null}
                    </button>
                  ))
                : null}
            </>
          ) : null}

          {modes.length > 0 ? (
            <>
              <SectionHeader title="模式" expanded={modeExpanded} onToggle={() => setModeExpanded((value) => !value)} topBorder={models.length > 0} value={displayedMode} />
              {modeExpanded
                ? modes.map((item, index) => (
                    <button
                      key={item.id}
                      type="button"
                      onClick={() => {
                        onModeChange?.(item.id);
                        closeMenu();
                      }}
                      title={item.description || item.id}
                      style={sectionItemStyle(item.id === displayedMode, index > 0)}
                    >
                      <span style={{ fontSize: "13px", fontWeight: 600 }}>{item.name || item.id}</span>
                      {item.description ? <span style={descriptionStyle}>{item.description}</span> : null}
                    </button>
                  ))
                : null}
            </>
          ) : null}

          {supportsEffort ? (
            <>
              <SectionHeader title="思考等级" expanded={effortExpanded} onToggle={() => setEffortExpanded((value) => !value)} topBorder={models.length > 0 || modes.length > 0} value={displayedEffort} />
              {effortExpanded
                ? efforts.map((item, index) => (
                    <button
                      key={item}
                      type="button"
                      onClick={() => {
                        onEffortChange?.(item);
                        closeMenu();
                      }}
                      style={sectionItemStyle(item === displayedEffort.toLowerCase(), index > 0)}
                    >
                      <span style={{ fontSize: "13px", fontWeight: 600, textTransform: "capitalize" }}>{item}</span>
                    </button>
                  ))
                : null}
            </>
          ) : null}

          {supportsFastService ? (
            <>
              <SectionHeader title="Fast 模式" expanded={serviceTierExpanded} onToggle={() => setServiceTierExpanded((value) => !value)} topBorder={models.length > 0 || modes.length > 0 || supportsEffort} value={fastModeEnabled ? "开启" : "关闭"} />
              {serviceTierExpanded
                ? (["off", "on"] as const).map((item, index) => (
                    <button
                      key={item}
                      type="button"
                      onClick={() => {
                        onFastServiceChange?.(item);
                        closeMenu();
                      }}
                      style={sectionItemStyle((item === "on") === fastModeEnabled, index > 0)}
                    >
                      <span style={{ fontSize: "13px", fontWeight: 600 }}>{item === "on" ? "开启" : "关闭"}</span>
                    </button>
                  ))
                : null}
            </>
          ) : null}
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

function SectionHeader({ title, expanded, onToggle, topBorder = false, value }: { title: string; expanded: boolean; onToggle: () => void; topBorder?: boolean; value?: string }) {
  return (
    <button
      type="button"
      onClick={onToggle}
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        width: "100%",
        minWidth: 0,
        padding: "10px 12px",
        border: "none",
        borderTop: topBorder ? "1px solid var(--menu-divider)" : "none",
        background: expanded ? "rgba(59,130,246,0.05)" : "transparent",
        color: "var(--text-primary)",
        cursor: "pointer",
      }}
    >
      <span style={{ fontSize: "11px", fontWeight: 700, color: expanded ? "#3b82f6" : "var(--text-secondary)" }}>{title}</span>
      <span style={{ display: "inline-flex", alignItems: "center", minWidth: 0, gap: "8px" }}>
        {value ? <span title={value} style={{ maxWidth: "130px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontSize: "11px", color: "var(--text-secondary)" }}>{value}</span> : null}
        <SelectorChevron expanded={expanded} side />
      </span>
    </button>
  );
}

function SelectorChevron({ expanded, side = false }: { expanded: boolean; side?: boolean }) {
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
        transform: side ? (expanded ? "rotate(90deg)" : "none") : expanded ? "rotate(180deg)" : "none",
        transition: "transform 0.16s ease",
      }}
    >
      <path d={side ? "M4 2.5 8 6 4 9.5" : "m2.5 4 3.5 4 3.5-4"} stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function sectionItemStyle(selected: boolean, topBorder = false, opacity = 1): React.CSSProperties {
  return {
    display: "flex",
    flexDirection: "column",
    alignItems: "flex-start",
    gap: "2px",
    width: "100%",
    minWidth: 0,
    padding: "10px 12px",
    border: "none",
    borderTop: topBorder ? "1px solid var(--menu-divider)" : "none",
    background: selected ? "rgba(59,130,246,0.08)" : "transparent",
    color: selected ? "#3b82f6" : "var(--text-primary)",
    textAlign: "left",
    cursor: "pointer",
    opacity,
  };
}
