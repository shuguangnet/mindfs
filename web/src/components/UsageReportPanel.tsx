import React, { useEffect, useMemo, useState } from "react";
import { useI18n } from "../i18n";
import {
  fetchUsagePreferences,
  fetchUsageReport,
  saveUsagePreferences,
  type UsageBudget,
  type UsageDayBucket,
  type UsageGroupBucket,
  type UsageReport,
  type UsageTotals,
} from "../services/usage";

type Props = {
  /** Restricts the report to one project; omitted means all projects. */
  rootId?: string | null;
  /** Projects available for the selector. */
  projects?: Array<{ id: string; name: string }>;
  onClose: () => void;
};

const RANGE_OPTIONS = [7, 30, 90] as const;
const COST_DECIMALS = 2;

/** Formats a USD amount compactly, keeping small amounts readable. */
function formatCost(value: number): string {
  if (!Number.isFinite(value) || value === 0) {
    return "$0.00";
  }
  if (Math.abs(value) < 0.01) {
    return "<$0.01";
  }
  return `$${value.toFixed(COST_DECIMALS)}`;
}

/** Formats a token count with a compact unit suffix. */
function formatTokens(value: number): string {
  const abs = Math.abs(value);
  if (abs >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`;
  if (abs >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`;
  if (abs >= 1_000) return `${(value / 1_000).toFixed(1)}k`;
  return String(value);
}

function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return "0%";
  return `${Math.round(value * 100)}%`;
}

/** Trend of the current window against the previous equally sized window. */
function trendLabel(current: number, previous: number): { text: string; tone: "up" | "down" | "flat" } {
  if (previous <= 0 && current <= 0) return { text: "—", tone: "flat" };
  if (previous <= 0) return { text: "新增", tone: "up" };
  const delta = (current - previous) / previous;
  if (Math.abs(delta) < 0.02) return { text: "≈0%", tone: "flat" };
  const sign = delta > 0 ? "+" : "";
  return { text: `${sign}${Math.round(delta * 100)}%`, tone: delta > 0 ? "up" : "down" };
}

const TONE_COLORS: Record<"up" | "down" | "flat", string> = {
  up: "#d97706",
  down: "#15803d",
  flat: "var(--text-secondary, #64748b)",
};

/**
 * Stacked area/bar chart of daily cost. Rendered with plain SVG so the report
 * adds no charting dependency.
 */
function CostTrendChart({
  days,
  onHover,
  hoveredDate,
}: {
  days: UsageDayBucket[];
  onHover: (date: string | null) => void;
  hoveredDate: string | null;
}) {
  const { t } = useI18n();
  const width = 720;
  const height = 160;
  const padding = { top: 12, right: 8, bottom: 20, left: 44 };
  const plotWidth = width - padding.left - padding.right;
  const plotHeight = height - padding.top - padding.bottom;

  const maxCost = Math.max(...days.map((day) => day.cost_usd), 0);
  // Keep an empty window from rendering a degenerate axis.
  const scaleMax = maxCost > 0 ? maxCost * 1.15 : 1;

  const barGap = days.length > 60 ? 0.5 : days.length > 30 ? 1 : 2;
  const slot = plotWidth / Math.max(days.length, 1);
  const barWidth = Math.max(slot - barGap, 1);

  const points = days.map((day, index) => {
    const x = padding.left + index * slot + barWidth / 2;
    const y = padding.top + plotHeight - (day.cost_usd / scaleMax) * plotHeight;
    return { x, y, day };
  });

  const areaPath = points.length
    ? `M ${points[0].x} ${padding.top + plotHeight} ` +
      points.map((point) => `L ${point.x} ${point.y}`).join(" ") +
      ` L ${points[points.length - 1].x} ${padding.top + plotHeight} Z`
    : "";
  const linePath = points.length
    ? `M ${points.map((point) => `${point.x} ${point.y}`).join(" L ")}`
    : "";

  const gridLines = [0, 0.5, 1];
  const labelEvery = Math.max(1, Math.ceil(days.length / 8));

  return (
    <div style={{ width: "100%", overflowX: "auto" }}>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        style={{ width: "100%", height: "170px", display: "block", minWidth: "420px" }}
        role="img"
        aria-label={t("usage.chartLabel")}
      >
        <defs>
          <linearGradient id="usage-area" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--accent-color, #3b82f6)" stopOpacity="0.32" />
            <stop offset="100%" stopColor="var(--accent-color, #3b82f6)" stopOpacity="0.02" />
          </linearGradient>
        </defs>

        {gridLines.map((ratio) => {
          const y = padding.top + plotHeight * ratio;
          const value = scaleMax * (1 - ratio);
          return (
            <g key={ratio}>
              <line
                x1={padding.left}
                x2={width - padding.right}
                y1={y}
                y2={y}
                stroke="var(--border-color, #e2e8f0)"
                strokeWidth="1"
                strokeDasharray={ratio === 1 ? undefined : "3 3"}
              />
              <text
                x={padding.left - 6}
                y={y + 3}
                textAnchor="end"
                fontSize="9"
                fill="var(--text-secondary, #64748b)"
              >
                {formatCost(value)}
              </text>
            </g>
          );
        })}

        {areaPath ? <path d={areaPath} fill="url(#usage-area)" /> : null}
        {linePath ? (
          <path
            d={linePath}
            fill="none"
            stroke="var(--accent-color, #3b82f6)"
            strokeWidth="1.75"
            strokeLinejoin="round"
            strokeLinecap="round"
          />
        ) : null}

        {points.map((point, index) =>
          point.day.cost_usd > 0 && days.length <= 45 ? (
            <circle
              key={point.day.date}
              cx={point.x}
              cy={point.y}
              r={hoveredDate === point.day.date ? 3.5 : 2}
              fill="var(--accent-color, #3b82f6)"
              stroke="var(--content-bg, #fff)"
              strokeWidth="1"
            />
          ) : null,
        )}

        {/* Invisible hover targets make the whole column interactive. */}
        {points.map((point) => (
          <rect
            key={`hit-${point.day.date}`}
            x={point.x - slot / 2}
            y={padding.top}
            width={slot}
            height={plotHeight}
            fill="transparent"
            onMouseEnter={() => onHover(point.day.date)}
            onMouseLeave={() => onHover(null)}
          />
        ))}

        {points.map((point, index) =>
          index % labelEvery === 0 ? (
            <text
              key={`label-${point.day.date}`}
              x={point.x}
              y={height - 6}
              textAnchor="middle"
              fontSize="9"
              fill="var(--text-secondary, #64748b)"
            >
              {point.day.date.slice(5)}
            </text>
          ) : null,
        )}
      </svg>
    </div>
  );
}

/** Horizontal share bars used for the agent/model/project breakdowns. */
function BreakdownTable({
  title,
  rows,
  totalCost,
  emptyText,
  showProjectDot,
}: {
  title: string;
  rows: UsageGroupBucket[];
  totalCost: number;
  emptyText: string;
  showProjectDot?: boolean;
}) {
  const maxCost = Math.max(...rows.map((row) => row.cost_usd), 0);
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "8px", minWidth: 0 }}>
      <div style={{ fontSize: "12px", fontWeight: 700, color: "var(--text-primary)" }}>{title}</div>
      {rows.length === 0 ? (
        <div style={{ fontSize: "11px", color: "var(--text-secondary)" }}>{emptyText}</div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
          {rows.map((row) => {
            const share = totalCost > 0 ? row.cost_usd / totalCost : 0;
            const barRatio = maxCost > 0 ? row.cost_usd / maxCost : 0;
            return (
              <div key={row.key} style={{ display: "flex", flexDirection: "column", gap: "3px", minWidth: 0 }}>
                <div style={{ display: "flex", alignItems: "baseline", gap: "6px", minWidth: 0 }}>
                  {showProjectDot ? (
                    <span
                      style={{
                        width: "6px",
                        height: "6px",
                        borderRadius: "999px",
                        background: "var(--accent-color, #3b82f6)",
                        opacity: 0.75,
                        flexShrink: 0,
                      }}
                    />
                  ) : null}
                  <span
                    title={row.label}
                    style={{
                      fontSize: "11.5px",
                      color: "var(--text-primary)",
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                      flex: 1,
                      minWidth: 0,
                    }}
                  >
                    {row.label}
                  </span>
                  <span
                    style={{
                      fontSize: "11px",
                      color: "var(--text-secondary)",
                      fontVariantNumeric: "tabular-nums",
                      flexShrink: 0,
                    }}
                  >
                    {formatTokens(row.total_tokens)}
                  </span>
                  <span
                    style={{
                      fontSize: "11.5px",
                      fontWeight: 600,
                      color: "var(--text-primary)",
                      fontVariantNumeric: "tabular-nums",
                      flexShrink: 0,
                      minWidth: "56px",
                      textAlign: "right",
                    }}
                  >
                    {formatCost(row.cost_usd)}
                  </span>
                  <span
                    style={{
                      fontSize: "10px",
                      color: "var(--text-secondary)",
                      fontVariantNumeric: "tabular-nums",
                      flexShrink: 0,
                      minWidth: "30px",
                      textAlign: "right",
                    }}
                  >
                    {formatPercent(share)}
                  </span>
                </div>
                <div
                  style={{
                    height: "3px",
                    borderRadius: "999px",
                    background: "rgba(148,163,184,0.16)",
                    overflow: "hidden",
                  }}
                >
                  <div
                    style={{
                      width: `${Math.max(barRatio * 100, row.cost_usd > 0 ? 2 : 0)}%`,
                      height: "100%",
                      borderRadius: "999px",
                      background: "var(--accent-color, #3b82f6)",
                      opacity: 0.7,
                    }}
                  />
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function MetricCard({
  label,
  value,
  hint,
  hintTone,
}: {
  label: string;
  value: string;
  hint?: string;
  hintTone?: "up" | "down" | "flat";
}) {
  return (
    <div
      style={{
        flex: "1 1 130px",
        minWidth: "130px",
        padding: "10px 12px",
        borderRadius: "10px",
        border: "1px solid var(--border-color)",
        background: "var(--menu-bg)",
        display: "flex",
        flexDirection: "column",
        gap: "2px",
      }}
    >
      <span style={{ fontSize: "10.5px", color: "var(--text-secondary)", letterSpacing: "0.02em" }}>
        {label}
      </span>
      <span
        style={{
          fontSize: "18px",
          fontWeight: 700,
          color: "var(--text-primary)",
          fontVariantNumeric: "tabular-nums",
          lineHeight: "24px",
        }}
      >
        {value}
      </span>
      {hint ? (
        <span
          style={{
            fontSize: "10.5px",
            color: hintTone ? TONE_COLORS[hintTone] : "var(--text-secondary)",
            fontVariantNumeric: "tabular-nums",
          }}
        >
          {hint}
        </span>
      ) : null}
    </div>
  );
}

/** Daily budget gauge with a colour ramp. */
function BudgetGauge({ report }: { report: UsageReport }) {
  const { t } = useI18n();
  if (report.budget.daily_usd <= 0) {
    return (
      <div style={{ fontSize: "11px", color: "var(--text-secondary)" }}>
        {t("usage.budgetUnset")}
      </div>
    );
  }
  const ratio = Math.max(0, Math.min(report.budget_ratio, 1.5));
  const percent = Math.min(ratio, 1);
  const color = report.budget_exceeded
    ? "#dc2626"
    : percent >= (report.budget.notify_percent || 80) / 100
      ? "#d97706"
      : "#15803d";
  const remaining = Math.max(0, report.budget.daily_usd - report.budget_used_usd);
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "5px", minWidth: "180px" }}>
      <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: "8px" }}>
        <span style={{ fontSize: "10.5px", color: "var(--text-secondary)" }}>{t("usage.budgetToday")}</span>
        <span style={{ fontSize: "11.5px", fontWeight: 600, color, fontVariantNumeric: "tabular-nums" }}>
          {formatCost(report.budget_used_usd)} / {formatCost(report.budget.daily_usd)}
        </span>
      </div>
      <div
        style={{
          height: "5px",
          borderRadius: "999px",
          background: "rgba(148,163,184,0.16)",
          overflow: "hidden",
        }}
      >
        <div
          style={{
            width: `${Math.min(percent * 100, 100)}%`,
            height: "100%",
            borderRadius: "999px",
            background: color,
            transition: "width 0.25s ease",
          }}
        />
      </div>
      <span style={{ fontSize: "10px", color: "var(--text-secondary)" }}>
        {report.budget_exceeded
          ? t("usage.budgetExceeded")
          : t("usage.budgetRemaining").replace("{amount}", formatCost(remaining))}
      </span>
    </div>
  );
}

/** Editor for per-model prices and the daily budget. */
function PriceEditor({
  report,
  onSaved,
}: {
  report: UsageReport;
  onSaved: () => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [budgetInput, setBudgetInput] = useState("");
  const [notifyPercent, setNotifyPercent] = useState("80");
  const [notifyOnExceed, setNotifyOnExceed] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  // Seed the editor from the models actually seen in the report, so the user
  // only fills in rates for models they really used.
  const models = useMemo(() => {
    const seen = new Set<string>(report.by_model.map((row) => row.key));
    for (const name of report.unpriced_models || []) {
      seen.add(name);
    }
    return Array.from(seen).sort();
  }, [report.by_model, report.unpriced_models]);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    fetchUsagePreferences()
      .then((prefs) => {
        if (cancelled) return;
        const next: Record<string, string> = {};
        for (const model of models) {
          const price = prefs.prices[model];
          next[`${model}:input`] = price ? String(price.input) : "";
          next[`${model}:output`] = price ? String(price.output) : "";
          next[`${model}:cache_read`] = price?.cache_read ? String(price.cache_read) : "";
        }
        setDraft(next);
        setBudgetInput(prefs.budget.daily_usd > 0 ? String(prefs.budget.daily_usd) : "");
        setNotifyPercent(String(prefs.budget.notify_percent || 80));
        setNotifyOnExceed(prefs.budget.notify_on_exceed !== false);
      })
      .catch(() => setError(t("usage.preferencesLoadFailed")));
    return () => {
      cancelled = true;
    };
  }, [open, models, t]);

  const handleSave = async () => {
    setSaving(true);
    setError("");
    try {
      const prices: Record<string, { input: number; output: number; cache_read?: number }> = {};
      for (const model of models) {
        const input = Number(draft[`${model}:input`] || 0);
        const output = Number(draft[`${model}:output`] || 0);
        const cacheRead = Number(draft[`${model}:cache_read`] || 0);
        if (!Number.isFinite(input) || !Number.isFinite(output)) continue;
        if (input === 0 && output === 0 && cacheRead === 0) continue;
        prices[model] = { input, output };
        if (cacheRead > 0) prices[model].cache_read = cacheRead;
      }
      const budget: UsageBudget = {
        daily_usd: Number(budgetInput || 0) || 0,
        notify_percent: Number(notifyPercent || 80) || 80,
        notify_on_exceed: notifyOnExceed,
      };
      await saveUsagePreferences({ prices, budget });
      setOpen(false);
      onSaved();
    } catch {
      setError(t("usage.preferencesSaveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const inputStyle: React.CSSProperties = {
    width: "100%",
    height: "26px",
    border: "1px solid var(--border-color)",
    borderRadius: "6px",
    background: "var(--input-bg, transparent)",
    color: "var(--text-primary)",
    fontSize: "11.5px",
    padding: "0 6px",
    outline: "none",
    fontVariantNumeric: "tabular-nums",
  };

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        style={{
          height: "26px",
          padding: "0 10px",
          borderRadius: "7px",
          border: "1px solid var(--border-color)",
          background: "transparent",
          color: "var(--text-secondary)",
          fontSize: "11.5px",
          cursor: "pointer",
          flexShrink: 0,
        }}
      >
        {t("usage.configurePrices")}
      </button>
    );
  }

  return (
    <div
      style={{
        position: "absolute",
        top: "42px",
        right: "14px",
        zIndex: 20,
        width: "min(460px, calc(100vw - 40px))",
        maxHeight: "min(520px, calc(100vh - 120px))",
        overflow: "auto",
        padding: "12px",
        borderRadius: "12px",
        border: "1px solid var(--border-color)",
        background: "var(--menu-bg)",
        boxShadow: "0 16px 40px rgba(15, 23, 42, 0.22)",
        display: "flex",
        flexDirection: "column",
        gap: "10px",
      }}
    >
      <div style={{ fontSize: "12px", fontWeight: 700, color: "var(--text-primary)" }}>
        {t("usage.pricesAndBudget")}
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
        <span style={{ fontSize: "10.5px", color: "var(--text-secondary)" }}>
          {t("usage.budgetHint")}
        </span>
        <div style={{ display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap" }}>
          <label style={{ display: "flex", alignItems: "center", gap: "5px" }}>
            <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>
              {t("usage.dailyBudget")}
            </span>
            <input
              type="number"
              min="0"
              step="0.5"
              value={budgetInput}
              onChange={(e) => setBudgetInput(e.target.value)}
              placeholder="0"
              style={{ ...inputStyle, width: "80px" }}
            />
          </label>
          <label style={{ display: "flex", alignItems: "center", gap: "5px" }}>
            <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>
              {t("usage.warnAt")}
            </span>
            <input
              type="number"
              min="1"
              max="100"
              value={notifyPercent}
              onChange={(e) => setNotifyPercent(e.target.value)}
              style={{ ...inputStyle, width: "60px" }}
            />
            <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>%</span>
          </label>
          <label style={{ display: "flex", alignItems: "center", gap: "5px", cursor: "pointer" }}>
            <input
              type="checkbox"
              checked={notifyOnExceed}
              onChange={(e) => setNotifyOnExceed(e.target.checked)}
            />
            <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>
              {t("usage.notifyOnExceed")}
            </span>
          </label>
        </div>
      </div>

      <div style={{ height: "1px", background: "var(--border-color)" }} />

      <div style={{ fontSize: "10.5px", color: "var(--text-secondary)" }}>
        {t("usage.priceHint")}
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
        <div style={{ display: "flex", gap: "6px", fontSize: "10px", color: "var(--text-secondary)" }}>
          <span style={{ flex: 1, minWidth: 0 }}>{t("usage.model")}</span>
          <span style={{ width: "62px" }}>{t("usage.input")}</span>
          <span style={{ width: "62px" }}>{t("usage.output")}</span>
          <span style={{ width: "62px" }}>{t("usage.cacheRead")}</span>
        </div>
        {models.length === 0 ? (
          <div style={{ fontSize: "11px", color: "var(--text-secondary)" }}>{t("usage.noModels")}</div>
        ) : (
          models.map((model) => (
            <div key={model} style={{ display: "flex", gap: "6px", alignItems: "center" }}>
              <span
                title={model}
                style={{
                  flex: 1,
                  minWidth: 0,
                  fontSize: "11px",
                  color: "var(--text-primary)",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                {model}
              </span>
              <input
                type="number"
                min="0"
                step="0.01"
                value={draft[`${model}:input`] ?? ""}
                onChange={(e) => setDraft((prev) => ({ ...prev, [`${model}:input`]: e.target.value }))}
                placeholder="0"
                style={{ ...inputStyle, width: "62px" }}
              />
              <input
                type="number"
                min="0"
                step="0.01"
                value={draft[`${model}:output`] ?? ""}
                onChange={(e) => setDraft((prev) => ({ ...prev, [`${model}:output`]: e.target.value }))}
                placeholder="0"
                style={{ ...inputStyle, width: "62px" }}
              />
              <input
                type="number"
                min="0"
                step="0.01"
                value={draft[`${model}:cache_read`] ?? ""}
                onChange={(e) => setDraft((prev) => ({ ...prev, [`${model}:cache_read`]: e.target.value }))}
                placeholder="—"
                style={{ ...inputStyle, width: "62px" }}
              />
            </div>
          ))
        )}
      </div>

      {error ? <div style={{ fontSize: "11px", color: "#dc2626" }}>{error}</div> : null}

      <div style={{ display: "flex", justifyContent: "flex-end", gap: "8px" }}>
        <button
          type="button"
          onClick={() => setOpen(false)}
          style={{
            height: "28px",
            padding: "0 12px",
            borderRadius: "7px",
            border: "1px solid var(--border-color)",
            background: "transparent",
            color: "var(--text-secondary)",
            fontSize: "12px",
            cursor: "pointer",
          }}
        >
          {t("usage.cancel")}
        </button>
        <button
          type="button"
          disabled={saving}
          onClick={() => void handleSave()}
          style={{
            height: "28px",
            padding: "0 12px",
            borderRadius: "7px",
            border: "none",
            background: "var(--accent-color, #3b82f6)",
            color: "var(--accent-foreground, #fff)",
            fontSize: "12px",
            fontWeight: 600,
            cursor: saving ? "default" : "pointer",
            opacity: saving ? 0.6 : 1,
          }}
        >
          {saving ? t("usage.saving") : t("usage.save")}
        </button>
      </div>
    </div>
  );
}

export function UsageReportPanel({ rootId, projects, onClose }: Props) {
  const { t } = useI18n();
  const [days, setDays] = useState<number>(30);
  const [scope, setScope] = useState<string>(rootId || "");
  const [report, setReport] = useState<UsageReport | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [hoveredDate, setHoveredDate] = useState<string | null>(null);

  useEffect(() => {
    setScope(rootId || "");
  }, [rootId]);

  const load = useMemo(
    () => async () => {
      setLoading(true);
      setError("");
      try {
        const next = await fetchUsageReport({ root: scope || undefined, days });
        setReport(next);
      } catch (err) {
        console.error("[usage] report failed", err);
        setError(t("usage.loadFailed"));
      } finally {
        setLoading(false);
      }
    },
    [scope, days, t],
  );

  useEffect(() => {
    void load();
  }, [load]);

  const hoveredBucket = report?.by_day.find((day) => day.date === hoveredDate) || null;
  const costTrend = report ? trendLabel(report.totals.cost_usd, report.previous_totals.cost_usd) : null;
  const tokenTrend = report ? trendLabel(report.totals.total_tokens, report.previous_totals.total_tokens) : null;

  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        zIndex: 30,
        background: "var(--mobile-overlay-bg, rgba(15,23,42,0.45))",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: "16px",
      }}
      onClick={onClose}
    >
      <div
        onClick={(event) => event.stopPropagation()}
        style={{
          position: "relative",
          width: "min(1000px, 100%)",
          maxHeight: "min(88vh, 900px)",
          overflow: "auto",
          borderRadius: "16px",
          border: "1px solid var(--border-color)",
          background: "var(--content-bg, var(--background))",
          boxShadow: "0 24px 60px rgba(15, 23, 42, 0.28)",
          display: "flex",
          flexDirection: "column",
        }}
      >
        {/* Header */}
        <div
          style={{
            position: "sticky",
            top: 0,
            zIndex: 5,
            display: "flex",
            alignItems: "center",
            gap: "10px",
            padding: "12px 14px",
            borderBottom: "1px solid var(--border-color)",
            background: "var(--content-bg, var(--background))",
            flexWrap: "wrap",
          }}
        >
          <div style={{ display: "flex", flexDirection: "column", minWidth: 0 }}>
            <span style={{ fontSize: "14px", fontWeight: 700, color: "var(--text-primary)" }}>
              {t("usage.title")}
            </span>
            <span style={{ fontSize: "10.5px", color: "var(--text-secondary)" }}>
              {report ? `${report.from} → ${report.to}` : t("usage.loading")}
            </span>
          </div>

          <div style={{ display: "flex", alignItems: "center", gap: "6px", marginLeft: "auto", flexWrap: "wrap" }}>
            {projects && projects.length > 0 ? (
              <select
                value={scope}
                onChange={(e) => setScope(e.target.value)}
                aria-label={t("usage.project")}
                style={{
                  height: "26px",
                  borderRadius: "7px",
                  border: "1px solid var(--border-color)",
                  background: "transparent",
                  color: "var(--text-primary)",
                  fontSize: "11.5px",
                  padding: "0 6px",
                  maxWidth: "170px",
                }}
              >
                <option value="">{t("usage.allProjects")}</option>
                {projects.map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.name}
                  </option>
                ))}
              </select>
            ) : null}

            <div
              style={{
                display: "inline-flex",
                border: "1px solid var(--border-color)",
                borderRadius: "7px",
                overflow: "hidden",
              }}
            >
              {RANGE_OPTIONS.map((option) => (
                <button
                  key={option}
                  type="button"
                  onClick={() => setDays(option)}
                  style={{
                    height: "26px",
                    padding: "0 10px",
                    border: "none",
                    background: days === option ? "var(--menu-active-bg, rgba(148,163,184,0.18))" : "transparent",
                    color: days === option ? "var(--text-primary)" : "var(--text-secondary)",
                    fontSize: "11.5px",
                    fontWeight: days === option ? 700 : 500,
                    cursor: "pointer",
                  }}
                >
                  {t("usage.daysShort").replace("{count}", String(option))}
                </button>
              ))}
            </div>

            <PriceEditor report={report || emptyReport} onSaved={() => void load()} />

            <button
              type="button"
              onClick={onClose}
              aria-label={t("usage.close")}
              style={{
                width: "26px",
                height: "26px",
                borderRadius: "7px",
                border: "1px solid var(--border-color)",
                background: "transparent",
                color: "var(--text-secondary)",
                cursor: "pointer",
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                flexShrink: 0,
              }}
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                <line x1="18" y1="6" x2="6" y2="18" />
                <line x1="6" y1="6" x2="18" y2="18" />
              </svg>
            </button>
          </div>
        </div>

        {/* Body */}
        <div style={{ padding: "14px", display: "flex", flexDirection: "column", gap: "14px" }}>
          {error ? (
            <div
              style={{
                padding: "10px 12px",
                borderRadius: "10px",
                border: "1px solid rgba(220,38,38,0.35)",
                background: "rgba(220,38,38,0.08)",
                color: "#dc2626",
                fontSize: "12px",
              }}
            >
              {error}
            </div>
          ) : null}

          {loading && !report ? (
            <div style={{ padding: "40px", textAlign: "center", color: "var(--text-secondary)", fontSize: "12px" }}>
              {t("usage.loading")}
            </div>
          ) : null}

          {report ? (
            <>
              {report.partial || (report.skipped_files || 0) > 0 ? (
                <div
                  style={{
                    padding: "8px 10px",
                    borderRadius: "10px",
                    border: "1px solid rgba(217,119,6,0.35)",
                    background: "rgba(217,119,6,0.08)",
                    color: "#b45309",
                    fontSize: "11.5px",
                  }}
                >
                  {t("usage.partialWarning")
                    .replace("{count}", String(report.skipped_files || 0))
                    .replace("{reason}", report.skipped_reason || t("usage.partialReasonUnknown"))}
                </div>
              ) : null}

              {/* Metric cards */}
              <div style={{ display: "flex", gap: "10px", flexWrap: "wrap" }}>
                <MetricCard
                  label={t("usage.totalCost")}
                  value={formatCost(report.totals.cost_usd)}
                  hint={costTrend ? `${t("usage.vsPrevious")} ${costTrend.text}` : undefined}
                  hintTone={costTrend?.tone}
                />
                <MetricCard
                  label={t("usage.totalTokens")}
                  value={formatTokens(report.totals.total_tokens)}
                  hint={tokenTrend ? `${t("usage.vsPrevious")} ${tokenTrend.text}` : undefined}
                  hintTone={tokenTrend?.tone}
                />
                <MetricCard
                  label={t("usage.turns")}
                  value={String(report.totals.turns)}
                  hint={t("usage.cacheHit").replace(
                    "{percent}",
                    report.totals.input > 0
                      ? formatPercent(report.totals.cache_read / report.totals.input)
                      : "0%",
                  )}
                />
                <MetricCard
                  label={t("usage.inputOutput")}
                  value={`${formatTokens(report.totals.input)} / ${formatTokens(report.totals.output)}`}
                  hint={t("usage.cacheReadWrite")
                    .replace("{read}", formatTokens(report.totals.cache_read))
                    .replace("{write}", formatTokens(report.totals.cache_write))}
                />
              </div>

              {/* Budget */}
              <div
                style={{
                  padding: "10px 12px",
                  borderRadius: "10px",
                  border: "1px solid var(--border-color)",
                  background: "var(--menu-bg)",
                }}
              >
                <BudgetGauge report={report} />
              </div>

              {/* Unpriced warning */}
              {report.unpriced_models && report.unpriced_models.length > 0 ? (
                <div
                  style={{
                    padding: "8px 10px",
                    borderRadius: "10px",
                    border: "1px solid rgba(148,163,184,0.35)",
                    background: "rgba(148,163,184,0.08)",
                    fontSize: "11.5px",
                    color: "var(--text-secondary)",
                  }}
                >
                  {t("usage.unpricedWarning").replace("{count}", String(report.unpriced_models.length))}
                  {" "}
                  <span style={{ color: "var(--text-primary)" }}>
                    {report.unpriced_models.slice(0, 4).join(", ")}
                    {report.unpriced_models.length > 4 ? " …" : ""}
                  </span>
                </div>
              ) : null}

              {/* Trend chart */}
              <div
                style={{
                  padding: "10px 12px",
                  borderRadius: "12px",
                  border: "1px solid var(--border-color)",
                  background: "var(--menu-bg)",
                  display: "flex",
                  flexDirection: "column",
                  gap: "6px",
                }}
              >
                <div style={{ display: "flex", alignItems: "baseline", gap: "8px" }}>
                  <span style={{ fontSize: "12px", fontWeight: 700, color: "var(--text-primary)" }}>
                    {t("usage.dailyCost")}
                  </span>
                  {hoveredBucket ? (
                    <span style={{ fontSize: "11px", color: "var(--text-secondary)", fontVariantNumeric: "tabular-nums" }}>
                      {hoveredBucket.date} · {formatCost(hoveredBucket.cost_usd)} ·{" "}
                      {formatTokens(hoveredBucket.total_tokens)} · {hoveredBucket.turns} {t("usage.turnsShort")}
                    </span>
                  ) : null}
                </div>
                <CostTrendChart days={report.by_day} onHover={setHoveredDate} hoveredDate={hoveredDate} />
              </div>

              {/* Breakdowns */}
              <div
                style={{
                  display: "grid",
                  gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))",
                  gap: "14px",
                }}
              >
                <div
                  style={{
                    padding: "10px 12px",
                    borderRadius: "12px",
                    border: "1px solid var(--border-color)",
                    background: "var(--menu-bg)",
                  }}
                >
                  <BreakdownTable
                    title={t("usage.byAgent")}
                    rows={report.by_agent}
                    totalCost={report.totals.cost_usd}
                    emptyText={t("usage.noData")}
                  />
                </div>
                <div
                  style={{
                    padding: "10px 12px",
                    borderRadius: "12px",
                    border: "1px solid var(--border-color)",
                    background: "var(--menu-bg)",
                  }}
                >
                  <BreakdownTable
                    title={t("usage.byModel")}
                    rows={report.by_model}
                    totalCost={report.totals.cost_usd}
                    emptyText={t("usage.noData")}
                  />
                </div>
                <div
                  style={{
                    padding: "10px 12px",
                    borderRadius: "12px",
                    border: "1px solid var(--border-color)",
                    background: "var(--menu-bg)",
                  }}
                >
                  <BreakdownTable
                    title={t("usage.byProject")}
                    rows={report.by_project}
                    totalCost={report.totals.cost_usd}
                    emptyText={t("usage.noData")}
                    showProjectDot
                  />
                </div>
              </div>

              <div style={{ fontSize: "10.5px", color: "var(--text-secondary)", textAlign: "right" }}>
                {t("usage.footer")
                  .replace("{records}", String(report.record_count))
                  .replace("{files}", String(report.scanned_file_count))}
              </div>
            </>
          ) : null}
        </div>
      </div>
    </div>
  );
}

/** Placeholder so the price editor renders before the first report arrives. */
const emptyReport: UsageReport = {
  from: "",
  to: "",
  days: 30,
  totals: emptyTotals(),
  today: emptyTotals(),
  budget: { daily_usd: 0 },
  budget_used_usd: 0,
  budget_ratio: 0,
  budget_exceeded: false,
  by_day: [],
  by_agent: [],
  by_model: [],
  by_project: [],
  previous_totals: emptyTotals(),
  price_configured: false,
  record_count: 0,
  scanned_file_count: 0,
};

function emptyTotals(): UsageTotals {
  return {
    turns: 0,
    input: 0,
    output: 0,
    cache_read: 0,
    cache_write: 0,
    total_tokens: 0,
    cost_usd: 0,
    unpriced_runs: 0,
  };
}
