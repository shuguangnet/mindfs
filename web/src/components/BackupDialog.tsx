import React from "react";
import { useI18n } from "../i18n";
import {
  applyBackupImport,
  downloadBlob,
  exportBackupBundle,
  importBackupBundle,
  type BackupApplyRoot,
  type BackupPreview,
  type BackupPreviewRoot,
} from "../services/backup";

type Props = {
  open: boolean;
  onClose: () => void;
};

type RootChoice = {
  action: BackupApplyRoot["action"];
  pathOverride: string;
};

const cardStyle: React.CSSProperties = {
  border: "1px solid var(--border-color)",
  borderRadius: "10px",
  padding: "14px",
  marginBottom: "14px",
  background: "var(--content-bg, #fff)",
};

const labelStyle: React.CSSProperties = {
  display: "block",
  fontSize: "11px",
  color: "var(--text-secondary)",
  marginBottom: "4px",
};

const inputStyle: React.CSSProperties = {
  width: "100%",
  boxSizing: "border-box",
  padding: "7px 9px",
  borderRadius: "8px",
  border: "1px solid var(--border-color)",
  background: "var(--bg-color, #fff)",
  color: "var(--text-primary)",
  fontSize: "12px",
};

const primaryButton: React.CSSProperties = {
  padding: "7px 14px",
  borderRadius: "8px",
  border: "none",
  background: "var(--accent-color)",
  color: "#fff",
  fontSize: "12px",
  cursor: "pointer",
};

const secondaryButton: React.CSSProperties = {
  padding: "7px 14px",
  borderRadius: "8px",
  border: "1px solid var(--border-color)",
  background: "transparent",
  color: "var(--text-primary)",
  fontSize: "12px",
  cursor: "pointer",
};

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

export function BackupDialog({ open, onClose }: Props) {
  const { t } = useI18n();
  const [exportPass, setExportPass] = React.useState("");
  const [includeUploads, setIncludeUploads] = React.useState(true);
  const [includeIdentity, setIncludeIdentity] = React.useState(true);
  const [exporting, setExporting] = React.useState(false);
  const [exportMsg, setExportMsg] = React.useState("");
  const [exportErr, setExportErr] = React.useState("");

  const [importFile, setImportFile] = React.useState<File | null>(null);
  const [importPass, setImportPass] = React.useState("");
  const [importing, setImporting] = React.useState(false);
  const [preview, setPreview] = React.useState<BackupPreview | null>(null);
  const [choices, setChoices] = React.useState<Record<string, RootChoice>>({});
  const [globalAction, setGlobalAction] = React.useState<"skip" | "replace">("replace");
  const [globalIdentity, setGlobalIdentity] = React.useState(true);
  const [applyMsg, setApplyMsg] = React.useState("");
  const [applyErr, setApplyErr] = React.useState("");
  const [applying, setApplying] = React.useState(false);

  React.useEffect(() => {
    if (!open) return;
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [open, onClose]);

  if (!open) return null;

  const resetImport = () => {
    setPreview(null);
    setChoices({});
    setImportFile(null);
    setImportPass("");
    setApplyMsg("");
    setApplyErr("");
  };

  const runExport = async () => {
    setExporting(true);
    setExportMsg("");
    setExportErr("");
    try {
      const { blob, filename } = await exportBackupBundle({
        passphrase: exportPass,
        include_uploads: includeUploads,
        include_device_identity: includeIdentity,
      });
      downloadBlob(blob, filename);
      setExportMsg(t("backup.exportDone", { size: formatBytes(blob.size) }));
    } catch (err) {
      setExportErr(String((err as Error)?.message || err));
    } finally {
      setExporting(false);
    }
  };

  const runImport = async () => {
    if (!importFile) return;
    setImporting(true);
    setApplyErr("");
    setApplyMsg("");
    try {
      const result = await importBackupBundle(importFile, importPass);
      setPreview(result);
      setGlobalIdentity(!!result.global?.device_identity_included);
      const next: Record<string, RootChoice> = {};
      for (const root of result.roots || []) {
        next[root.id] = {
          action: root.status === "exists_same_path" ? "merge" : root.status === "path_missing_on_disk" ? "skip" : "replace",
          pathOverride: "",
        };
      }
      setChoices(next);
    } catch (err) {
      setApplyErr(String((err as Error)?.message || err));
    } finally {
      setImporting(false);
    }
  };

  const runApply = async () => {
    if (!preview) return;
    setApplying(true);
    setApplyErr("");
    setApplyMsg("");
    try {
      const roots: BackupApplyRoot[] = (preview.roots || []).map((root) => ({
        id: root.id,
        action: choices[root.id]?.action || "skip",
        path_override: (choices[root.id]?.pathOverride || "").trim() || undefined,
      }));
      const result = await applyBackupImport(preview.token, roots, globalAction, globalIdentity);
      const parts: string[] = [];
      for (const row of result.roots || []) {
        if (row.action === "skip") continue;
        parts.push(`${row.root_path || row.id}: ${row.action === "merge" ? `+${row.sessions_merged}` : `+${row.files_copied}`}`);
      }
      setApplyMsg(
        t("backup.applyDone", {
          detail: parts.join(" · ") || t("backup.globalOnly"),
          global: String(result.global_files_replaced || 0),
        }),
      );
      if (result.reload_needed) {
        setApplyMsg((m) => `${m} ${t("backup.restartHint")}`);
      }
      setPreview(null);
      setImportFile(null);
      setImportPass("");
    } catch (err) {
      setApplyErr(String((err as Error)?.message || err));
    } finally {
      setApplying(false);
    }
  };

  const statusLabel = (status: BackupPreviewRoot["status"]): string => {
    switch (status) {
      case "exists_same_path":
        return t("backup.statusExists");
      case "path_missing_on_disk":
        return t("backup.statusMissing");
      default:
        return t("backup.statusNew");
    }
  };

  return (
    <div
      data-backup-overlay
      style={{
        position: "fixed",
        inset: 0,
        background: "rgba(15, 23, 42, 0.45)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        zIndex: 1000,
        padding: "16px",
      }}
      onClick={onClose}
    >
      <div
        style={{
          width: "min(680px, 100%)",
          maxHeight: "86vh",
          overflowY: "auto",
          borderRadius: "14px",
          border: "1px solid var(--border-color)",
          background: "var(--bg-color, #fff)",
          padding: "18px",
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: "14px" }}>
          <h2 style={{ margin: 0, fontSize: "15px" }}>{t("backup.title")}</h2>
          <button type="button" onClick={onClose} style={{ ...secondaryButton, padding: "4px 10px" }} aria-label="close">
            ✕
          </button>
        </div>

        {/* Export card */}
        <div style={cardStyle}>
          <h3 style={{ margin: "0 0 8px", fontSize: "13px" }}>{t("backup.exportTitle")}</h3>
          <p style={{ margin: "0 0 10px", fontSize: "11px", color: "var(--text-secondary)" }}>{t("backup.exportHint")}</p>
          <label style={labelStyle}>{t("backup.passphrase")}</label>
          <input
            type="password"
            style={inputStyle}
            value={exportPass}
            onChange={(e) => setExportPass(e.target.value)}
            placeholder={t("backup.passphrasePlaceholder")}
            autoComplete="new-password"
          />
          <label style={{ ...labelStyle, display: "flex", alignItems: "center", gap: "6px", marginTop: "8px", cursor: "pointer" }}>
            <input type="checkbox" checked={includeUploads} onChange={(e) => setIncludeUploads(e.target.checked)} />
            {t("backup.includeUploads")}
          </label>
          <label style={{ ...labelStyle, display: "flex", alignItems: "center", gap: "6px", cursor: "pointer" }}>
            <input type="checkbox" checked={includeIdentity} onChange={(e) => setIncludeIdentity(e.target.checked)} />
            {t("backup.includeIdentity")}
          </label>
          <div style={{ marginTop: "10px", display: "flex", gap: "8px", alignItems: "center" }}>
            <button
              type="button"
              style={primaryButton}
              disabled={exporting || exportPass.trim().length < 8}
              onClick={() => void runExport()}
            >
              {exporting ? t("backup.working") : t("backup.exportButton")}
            </button>
            {exportMsg ? <span style={{ fontSize: "11px", color: "#16a34a" }}>{exportMsg}</span> : null}
            {exportErr ? <span style={{ fontSize: "11px", color: "#dc2626" }}>{exportErr}</span> : null}
          </div>
        </div>

        {/* Import card */}
        <div style={cardStyle}>
          <h3 style={{ margin: "0 0 8px", fontSize: "13px" }}>{t("backup.importTitle")}</h3>
          <p style={{ margin: "0 0 10px", fontSize: "11px", color: "var(--text-secondary)" }}>{t("backup.importHint")}</p>
          <label style={labelStyle}>{t("backup.bundleFile")}</label>
          <input
            type="file"
            style={inputStyle}
            accept=".mbk,application/octet-stream"
            onChange={(e) => {
              setImportFile(e.target.files?.[0] || null);
              setPreview(null);
              setApplyMsg("");
              setApplyErr("");
            }}
          />
          <label style={{ ...labelStyle, marginTop: "8px" }}>{t("backup.passphrase")}</label>
          <input
            type="password"
            style={inputStyle}
            value={importPass}
            onChange={(e) => setImportPass(e.target.value)}
            placeholder={t("backup.passphrasePlaceholder")}
            autoComplete="off"
          />
          <div style={{ marginTop: "10px", display: "flex", gap: "8px", alignItems: "center" }}>
            <button
              type="button"
              style={primaryButton}
              disabled={importing || !importFile || importPass.trim().length < 8}
              onClick={() => void runImport()}
            >
              {importing ? t("backup.working") : t("backup.decryptPreview")}
            </button>
            {preview ? <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>{t("backup.previewReady")}</span> : null}
            {applyErr ? <span style={{ fontSize: "11px", color: "#dc2626", maxWidth: "300px" }}>{applyErr}</span> : null}
          </div>

          {applyMsg ? (
            <div style={{ marginTop: "10px", fontSize: "12px", color: "#16a34a", whiteSpace: "pre-wrap" }}>{applyMsg}</div>
          ) : null}

          {preview ? (
            <div style={{ marginTop: "12px" }}>
              <div style={{ fontSize: "11px", color: "var(--text-secondary)", marginBottom: "8px" }}>
                {t("backup.previewMeta", {
                  date: new Date(preview.created_at).toLocaleString(),
                  version: preview.mindfs_version || "-",
                  global: formatBytes(preview.global?.bytes || 0),
                })}
              </div>
              {(preview.warnings || []).length > 0 ? (
                <div style={{ fontSize: "11px", color: "#d97706", marginBottom: "8px" }}>
                  {preview.warnings!.map((w, i) => (
                    <div key={i}>⚠ {w}</div>
                  ))}
                </div>
              ) : null}
              <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "11px" }}>
                <thead>
                  <tr style={{ textAlign: "left", color: "var(--text-secondary)" }}>
                    <th style={{ padding: "4px 6px" }}>{t("backup.colProject")}</th>
                    <th style={{ padding: "4px 6px" }}>{t("backup.colStatus")}</th>
                    <th style={{ padding: "4px 6px" }}>{t("backup.colAction")}</th>
                    <th style={{ padding: "4px 6px" }}>{t("backup.colPathOverride")}</th>
                  </tr>
                </thead>
                <tbody>
                  {(preview.roots || []).map((root) => {
                    const choice = choices[root.id] || { action: "skip" as const, pathOverride: "" };
                    return (
                      <tr key={root.id} style={{ borderTop: "1px solid var(--border-color)" }}>
                        <td style={{ padding: "4px 6px" }}>
                          <div style={{ fontWeight: 600 }}>{root.name}</div>
                          <div style={{ color: "var(--text-secondary)" }}>{root.root_path}</div>
                          <div style={{ color: "var(--text-secondary)" }}>
                            {formatBytes(root.bytes)} · {root.file_count} files
                            {root.has_tasks ? ` · ${t("backup.hasTasks")}` : ""}
                          </div>
                        </td>
                        <td style={{ padding: "4px 6px" }}>{statusLabel(root.status)}</td>
                        <td style={{ padding: "4px 6px" }}>
                          <select
                            value={choice.action}
                            onChange={(e) =>
                              setChoices((prev) => ({
                                ...prev,
                                [root.id]: { ...choice, action: e.target.value as BackupApplyRoot["action"] },
                              }))
                            }
                            style={inputStyle}
                          >
                            <option value="merge">{t("backup.actionMerge")}</option>
                            <option value="replace">{t("backup.actionReplace")}</option>
                            <option value="skip">{t("backup.actionSkip")}</option>
                          </select>
                        </td>
                        <td style={{ padding: "4px 6px" }}>
                          <input
                            style={inputStyle}
                            placeholder={t("backup.pathOverridePlaceholder")}
                            value={choice.pathOverride}
                            onChange={(e) =>
                              setChoices((prev) => ({
                                ...prev,
                                [root.id]: { ...choice, pathOverride: e.target.value },
                              }))
                            }
                          />
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              <div style={{ marginTop: "10px", display: "flex", alignItems: "center", gap: "10px", flexWrap: "wrap" }}>
                <label style={{ ...labelStyle, display: "flex", alignItems: "center", gap: "6px", cursor: "pointer", margin: 0 }}>
                  {t("backup.globalConfig")}
                  <select value={globalAction} onChange={(e) => setGlobalAction(e.target.value as "skip" | "replace")} style={{ ...inputStyle, width: "auto" }}>
                    <option value="replace">{t("backup.actionReplace")}</option>
                    <option value="skip">{t("backup.actionSkip")}</option>
                  </select>
                </label>
                <label style={{ ...labelStyle, display: "flex", alignItems: "center", gap: "6px", cursor: "pointer", margin: 0 }}>
                  <input type="checkbox" checked={globalIdentity} onChange={(e) => setGlobalIdentity(e.target.checked)} />
                  {t("backup.includeIdentity")}
                </label>
              </div>
              <div style={{ marginTop: "12px", display: "flex", gap: "8px" }}>
                <button type="button" style={primaryButton} disabled={applying} onClick={() => void runApply()}>
                  {applying ? t("backup.working") : t("backup.applyButton")}
                </button>
                <button type="button" style={secondaryButton} onClick={resetImport}>
                  {t("backup.cancelImport")}
                </button>
              </div>
              <p style={{ margin: "8px 0 0", fontSize: "11px", color: "var(--text-secondary)" }}>{t("backup.applyHint")}</p>
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
