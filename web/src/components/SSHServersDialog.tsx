import React from "react";
import { useI18n } from "../i18n";
import { SSHKeyFilePicker } from "./SSHKeyFilePicker";
import {
  applySSHImport,
  deleteSSHServer,
  deploySSHServerKey,
  downloadSSHExportFile,
  exportSSHServers,
  fetchSSHServers,
  previewSSHImport,
  saveSSHServer,
  testSSHServer,
  type SSHAuthMode,
  type SSHImportPreview,
  type SSHListResponse,
  type SSHReuseInfo,
  type SSHSaveInput,
  type SSHServer,
  type SSHTestResult,
} from "../services/sshServers";

type Props = {
  open: boolean;
  onClose: () => void;
};

type FormState = SSHSaveInput & {
  new_password: string;
  new_key_content: string;
  key_path: string;
  proxy_jump: string;
  notes: string;
  new_key_password_mode: "set" | "reuse" | "keep";
  reuse_password_from: string;
  reuse_key_from: string;
};

const emptyForm: FormState = {
  id: "",
  alias: "",
  host: "",
  port: 22,
  user: "root",
  auth: "password",
  key_path: "",
  proxy_jump: "",
  enabled: true,
  notes: "",
  new_password: "",
  new_key_content: "",
  new_key_password_mode: "set",
  reuse_password_from: "",
  reuse_key_from: "",
};

const reasonCodeKeys: Record<string, { key: string }> = {
  network_unreachable: { key: "sshServers.reasonNetwork" },
  timeout: { key: "sshServers.reasonTimeout" },
  auth_rejected: { key: "sshServers.reasonAuth" },
  key_unreadable: { key: "sshServers.reasonKey" },
  host_key_rejected: { key: "sshServers.reasonHostKey" },
  dial_failed: { key: "sshServers.reasonDial" },
  secret_missing: { key: "sshServers.reasonSecret" },
};

export function SSHServersDialog({ open, onClose }: Props) {
  const { t } = useI18n();
  const [list, setList] = React.useState<SSHListResponse | null>(null);
  const [form, setForm] = React.useState<FormState>(emptyForm);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [summary, setSummary] = React.useState("");
  const [importOpen, setImportOpen] = React.useState(false);
  const [importPreview, setImportPreview] = React.useState<SSHImportPreview | null>(null);
  const [importPayload, setImportPayload] = React.useState("");
  const [importSource, setImportSource] = React.useState<"json" | "ssh_config">("json");
  const [importPassphrase, setImportPassphrase] = React.useState("");
  const [importResolutions, setImportResolutions] = React.useState<Record<string, "overwrite" | "suffix" | "skip">>({});
  const [exportPassphrase, setExportPassphrase] = React.useState("");
  const [keyPickerOpen, setKeyPickerOpen] = React.useState(false);
  const fileInputRef = React.useRef<HTMLInputElement | null>(null);

  const load = React.useCallback(async () => {
    try {
      const payload = await fetchSSHServers();
      setList(payload);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("sshServers.loadFailed"));
    }
  }, [t]);

  React.useEffect(() => {
    if (open) {
      void load();
    }
  }, [load, open]);

  if (!open) return null;

  const update = (patch: Partial<FormState>) => {
    setForm((prev) => ({ ...prev, ...patch }));
    setSummary("");
    setError("");
  };

  const selectServer = (server: SSHServer) => {
    setForm({
      ...emptyForm,
      id: server.id,
      alias: server.alias,
      host: server.host,
      port: server.port || 22,
      user: server.user,
      auth: server.auth,
      key_path: server.key_path || "",
      proxy_jump: server.proxy_jump || "",
      enabled: server.enabled,
      notes: server.notes || "",
      new_key_password_mode: "keep",
    });
    setSummary("");
    setError("");
  };

  const reuse: SSHReuseInfo = list?.reuse ?? { key_paths: [], passwords: [], managed_keys: [] };
  const selected = list?.servers.find((item) => item.id === form.id);

  const save = async () => {
    setBusy(true);
    setError("");
    try {
      const input: SSHSaveInput = {
        id: form.id,
        alias: form.alias.trim(),
        host: form.host.trim(),
        port: Number(form.port) || 22,
        user: form.user.trim(),
        auth: form.auth,
        key_path: form.auth === "key_path" ? (form.key_path || "").trim() : "",
        proxy_jump: (form.proxy_jump || "").trim(),
        enabled: form.enabled,
        notes: form.notes || "",
      };
      if (form.auth === "password") {
        if (form.reuse_password_from) {
          input.reuse_password_from = form.reuse_password_from;
        } else if (form.new_password) {
          input.new_password = form.new_password;
        }
      }
      if (form.auth === "key_inline") {
        if (form.reuse_key_from) {
          input.reuse_key_from = form.reuse_key_from;
        } else if (form.new_key_content) {
          input.new_key_content = form.new_key_content;
        }
      }
      const result = await saveSSHServer(input);
      setSummary(result.warning ? t("sshServers.savedWithWarning", { warning: result.warning }) : t("sshServers.saved"));
      setForm((prev) => ({ ...prev, id: result.server.id, new_password: "", new_key_content: "", reuse_password_from: "", reuse_key_from: "" }));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("sshServers.saveFailed"));
    } finally {
      setBusy(false);
    }
  };

  const test = async () => {
    if (!form.id) {
      setError(t("sshServers.saveFirst"));
      return;
    }
    setBusy(true);
    setError("");
    setSummary(t("sshServers.testing"));
    try {
      const result: SSHTestResult = await testSSHServer(form.id);
      if (result.ok) {
        setSummary(t("sshServers.testOk", { banner: result.banner || "" }));
      } else {
        const reasonKey = reasonCodeKeys[result.reason_code || ""]?.key;
        const reason = reasonKey ? t(reasonKey as never) : result.reason_code || "";
        setError(`${t("sshServers.testFailed")}${reason ? ` · ${reason}` : ""}${result.detail ? ` (${result.detail})` : ""}`);
        setSummary("");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("sshServers.testFailed"));
    } finally {
      setBusy(false);
    }
  };

  const deployKey = async () => {
    if (!form.id) {
      setError(t("sshServers.saveFirst"));
      return;
    }
    setBusy(true);
    setError("");
    setSummary(t("sshServers.deploying"));
    try {
      const result = await deploySSHServerKey(form.id);
      if (result.ok) {
        setSummary(result.warning ? t("sshServers.deployedWithWarning", { warning: result.warning }) : t("sshServers.deployed"));
      } else {
        setError(t("sshServers.deployFailed") + (result.detail ? ` (${result.detail})` : ""));
      }
      await load();
      const updated = (await fetchSSHServers()).servers.find((item) => item.id === form.id);
      if (updated) {
        selectServer(updated);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("sshServers.deployFailed"));
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!form.id) return;
    setBusy(true);
    setError("");
    try {
      await deleteSSHServer(form.id);
      setForm(emptyForm);
      setSummary(t("sshServers.deleted"));
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("sshServers.deleteFailed"));
    } finally {
      setBusy(false);
    }
  };

  const startImportFile = () => {
    setImportOpen(true);
    setImportSource("json");
    setImportPreview(null);
    setImportPayload("");
    setImportPassphrase("");
    setImportResolutions({});
    fileInputRef.current?.click();
  };

  const onImportFile = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    const text = await file.text();
    setImportPayload(text);
    await runImportPreview("json", text);
  };

  const importLocalConfig = async () => {
    setImportOpen(true);
    setImportSource("ssh_config");
    setImportPreview(null);
    setImportPayload("");
    setImportPassphrase("");
    setImportResolutions({});
    setBusy(true);
    setError("");
    try {
      const preview = await previewSSHImport("ssh_config", "");
      setImportPreview(preview);
      setImportSource("ssh_config");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("sshServers.importFailed"));
    } finally {
      setBusy(false);
    }
  };

  const runImportPreview = async (source: "json" | "ssh_config", payload: string, passphrase = "") => {
    setBusy(true);
    setError("");
    try {
      const preview = await previewSSHImport(source, payload, passphrase);
      setImportPreview(preview);
      setImportSource(source);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("sshServers.importFailed"));
      setImportPreview(null);
    } finally {
      setBusy(false);
    }
  };

  const applyImport = async () => {
    setBusy(true);
    setError("");
    try {
      const result = await applySSHImport(importSource, importPayload, importPassphrase, importResolutions);
      setSummary(t("sshServers.importApplied", { created: result.created, updated: result.updated, skipped: result.skipped }));
      setImportOpen(false);
      setImportPreview(null);
      setImportPayload("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("sshServers.importFailed"));
    } finally {
      setBusy(false);
    }
  };

  const runExport = async (mode: "encrypted" | "no-secrets") => {
    setBusy(true);
    setError("");
    try {
      const file = await exportSSHServers(mode, exportPassphrase);
      downloadSSHExportFile(file);
      setSummary(t("sshServers.exported"));
    } catch (err) {
      setError(err instanceof Error ? err.message : t("sshServers.exportFailed"));
    } finally {
      setBusy(false);
    }
  };

  const authBadge = (server: SSHServer) => {
    if (server.auth === "password") return t("sshServers.authPassword");
    if (server.auth === "key_path") return t("sshServers.authKeyPath");
    return t("sshServers.authManagedKey");
  };

  // Start the picker in the directory that contains the current key path
  // (a bare file path would fail directory listing).
  const keyPickerStart = (() => {
    const p = (form.key_path || "").trim();
    if (!p) return "";
    const tilde = p.startsWith("~");
    const body = tilde ? p.slice(1) : p;
    const idx = body.lastIndexOf("/");
    if (idx < 0) return "";
    if (idx === 0) return tilde ? "~/" : "/";
    return (tilde ? "~" : "") + body.slice(0, idx);
  })();

  return (
    <div
      style={{
        border: "1px solid var(--border-color)",
        background: "var(--menu-bg)",
        borderRadius: "10px",
        boxShadow: "0 18px 45px rgba(15, 23, 42, 0.18)",
        padding: "12px",
        color: "var(--text-primary)",
        maxHeight: "70vh",
        overflowY: "auto",
      }}
    >
      <input
        ref={fileInputRef}
        type="file"
        accept=".json,application/json"
        style={{ display: "none" }}
        onChange={(event) => void onImportFile(event)}
      />
      <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "10px" }}>
        <div style={{ fontSize: "13px", fontWeight: 700, flex: 1 }}>{t("sshServers.title")}</div>
        <button type="button" onClick={() => { setForm(emptyForm); setSummary(""); setError(""); }} style={iconButtonStyle} aria-label={t("sshServers.newServer")}>
          +
        </button>
        <button type="button" onClick={onClose} style={iconButtonStyle} aria-label={t("common.close")}>
          x
        </button>
      </div>

      {list && list.servers.length > 0 ? (
        <div style={{ display: "flex", gap: "6px", overflowX: "auto", marginBottom: "10px", flexWrap: "wrap" }}>
          {list.servers.map((server) => (
            <button
              key={server.id}
              type="button"
              onClick={() => selectServer(server)}
              style={{
                border: "1px solid var(--border-color)",
                background: server.id === form.id ? "rgba(59,130,246,0.12)" : "transparent",
                color: "var(--text-primary)",
                borderRadius: "8px",
                padding: "6px 8px",
                fontSize: "12px",
                cursor: "pointer",
                whiteSpace: "nowrap",
                opacity: server.enabled ? 1 : 0.55,
              }}
              title={`${server.user}@${server.host}:${server.port}`}
            >
              {server.alias}
              <span style={{ marginLeft: "6px", color: "var(--text-secondary)", fontSize: "10px" }}>{authBadge(server)}</span>
            </button>
          ))}
        </div>
      ) : null}

      {list?.materialize?.warning ? (
        <div style={{ color: "#d97706", fontSize: "11px", marginBottom: "8px" }}>{list.materialize.warning}</div>
      ) : null}

      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "8px" }}>
        <Field label={t("sshServers.alias")} value={form.alias} onChange={(value) => update({ alias: value })} placeholder="crunchbits" />
        <Field label={t("sshServers.host")} value={form.host} onChange={(value) => update({ host: value })} placeholder="203.0.113.10" />
        <Field label={t("sshServers.port")} value={String(form.port)} onChange={(value) => update({ port: Number(value) || 22 })} placeholder="22" />
        <Field label={t("sshServers.user")} value={form.user} onChange={(value) => update({ user: value })} placeholder="root" />
      </div>

      <div style={{ display: "flex", gap: "6px", marginTop: "8px" }}>
        {(["password", "key_path", "key_inline"] as SSHAuthMode[]).map((mode) => (
          <button
            key={mode}
            type="button"
            onClick={() => update({ auth: mode, new_password: "", new_key_content: "", reuse_password_from: "", reuse_key_from: "" })}
            style={{
              flex: 1,
              border: "1px solid var(--border-color)",
              background: form.auth === mode ? "rgba(59,130,246,0.12)" : "transparent",
              color: "var(--text-primary)",
              borderRadius: "8px",
              padding: "6px 4px",
              fontSize: "11px",
              cursor: "pointer",
            }}
          >
            {mode === "password" ? t("sshServers.authPassword") : mode === "key_path" ? t("sshServers.authKeyPath") : t("sshServers.authManagedKey")}
          </button>
        ))}
      </div>

      {form.auth === "password" ? (
        <div style={{ marginTop: "8px", display: "grid", gap: "6px" }}>
          {reuse.passwords.filter((item) => item.id !== form.id).length > 0 ? (
            <label style={{ display: "grid", gap: "4px" }}>
              <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>{t("sshServers.reusePassword")}</span>
              <select
                value={form.reuse_password_from}
                onChange={(event) => update({ reuse_password_from: event.target.value, new_password: "" })}
                style={inputStyle}
              >
                <option value="">{t("sshServers.reuseNone")}</option>
                {reuse.passwords.filter((item) => item.id !== form.id).map((item) => (
                  <option key={item.id} value={item.id}>{item.alias}</option>
                ))}
              </select>
            </label>
          ) : null}
          <Field
            label={t("sshServers.password")}
            value={form.new_password}
            onChange={(value) => update({ new_password: value, reuse_password_from: "" })}
            placeholder={selected?.has_password ? t("sshServers.keepPassword") : undefined}
            type="password"
          />
        </div>
      ) : null}

      {form.auth === "key_path" ? (
        <div style={{ marginTop: "8px", display: "grid", gap: "6px" }}>
          <label style={{ display: "grid", gap: "4px" }}>
            <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>{t("sshServers.keyPath")}</span>
            <div style={{ display: "flex", gap: "6px" }}>
              <input
                value={form.key_path || ""}
                onChange={(event) => update({ key_path: event.target.value })}
                placeholder="~/.ssh/id_ed25519"
                spellCheck={false}
                style={{ ...inputStyle, flex: 1, fontFamily: "monospace" }}
              />
              <button
                type="button"
                onClick={() => setKeyPickerOpen((prev) => !prev)}
                style={{ ...secondaryButtonStyle, whiteSpace: "nowrap" }}
              >
                {t("sshServers.fsBrowse")}
              </button>
            </div>
          </label>
          {keyPickerOpen ? (
            <SSHKeyFilePicker
              initialPath={keyPickerStart}
              onSelect={(path) => {
                update({ key_path: path });
                setKeyPickerOpen(false);
              }}
              onClose={() => setKeyPickerOpen(false)}
            />
          ) : null}
          {reuse.key_paths.filter((path) => path !== form.key_path).length > 0 ? (
            <label style={{ display: "grid", gap: "4px" }}>
              <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>{t("sshServers.reuseKeyPath")}</span>
              <select value="" onChange={(event) => event.target.value && update({ key_path: event.target.value })} style={inputStyle}>
                <option value="">{t("sshServers.reuseNone")}</option>
                {reuse.key_paths.filter((path) => path !== form.key_path).map((path) => (
                  <option key={path} value={path}>{path}</option>
                ))}
              </select>
            </label>
          ) : null}
        </div>
      ) : null}

      {form.auth === "key_inline" ? (
        <div style={{ marginTop: "8px", display: "grid", gap: "6px" }}>
          {reuse.managed_keys.filter((item) => item.id !== form.id).length > 0 ? (
            <label style={{ display: "grid", gap: "4px" }}>
              <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>{t("sshServers.reuseManagedKey")}</span>
              <select
                value={form.reuse_key_from}
                onChange={(event) => update({ reuse_key_from: event.target.value, new_key_content: "" })}
                style={inputStyle}
              >
                <option value="">{t("sshServers.reuseNone")}</option>
                {reuse.managed_keys.filter((item) => item.id !== form.id).map((item) => (
                  <option key={item.id} value={item.id}>{item.alias}</option>
                ))}
              </select>
            </label>
          ) : null}
          <label style={{ display: "grid", gap: "4px" }}>
            <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>
              {t("sshServers.keyContent")}{selected?.key_source === "managed" ? t("sshServers.keepKeyHint") : ""}
            </span>
            <textarea
              value={form.new_key_content}
              onChange={(event) => update({ new_key_content: event.target.value, reuse_key_from: "" })}
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
              style={{ ...inputStyle, minHeight: "72px", fontFamily: "monospace" }}
              spellCheck={false}
            />
          </label>
        </div>
      ) : null}

      <Field label={t("sshServers.proxyJump")} value={form.proxy_jump || ""} onChange={(value) => update({ proxy_jump: value })} placeholder="optional" />
      <label style={{ display: "flex", gap: "8px", alignItems: "center", fontSize: "12px", marginTop: "10px" }}>
        <input type="checkbox" checked={form.enabled} onChange={(event) => update({ enabled: event.target.checked })} />
        {t("sshServers.enabled")}
      </label>

      {error ? <div style={{ color: "#ef4444", fontSize: "12px", marginTop: "8px", wordBreak: "break-all" }}>{error}</div> : null}
      {summary ? <div style={{ color: "var(--text-secondary)", fontSize: "12px", marginTop: "8px", wordBreak: "break-all" }}>{summary}</div> : null}

      <div style={{ display: "flex", gap: "8px", justifyContent: "flex-end", marginTop: "12px", flexWrap: "wrap" }}>
        <button type="button" onClick={startImportFile} disabled={busy} style={secondaryButtonStyle}>
          {t("sshServers.importFile")}
        </button>
        <button type="button" onClick={() => void importLocalConfig()} disabled={busy} style={secondaryButtonStyle}>
          {t("sshServers.importLocal")}
        </button>
        <button type="button" onClick={() => void runExport(exportPassphrase ? "encrypted" : "no-secrets")} disabled={busy} style={secondaryButtonStyle}>
          {t("sshServers.export")}
        </button>
      </div>
      <div style={{ display: "flex", gap: "8px", marginTop: "6px" }}>
        <input
          value={exportPassphrase}
          onChange={(event) => setExportPassphrase(event.target.value)}
          placeholder={t("sshServers.exportPassphrase")}
          style={{ ...inputStyle, flex: 1 }}
          type="password"
        />
      </div>

      <div style={{ display: "flex", gap: "8px", justifyContent: "flex-end", marginTop: "12px" }}>
        {form.id ? (
          <button type="button" onClick={() => void remove()} disabled={busy} style={dangerButtonStyle}>
            {t("common.delete")}
          </button>
        ) : null}
        {form.id && form.auth === "password" ? (
          <button type="button" onClick={() => void deployKey()} disabled={busy} style={secondaryButtonStyle}>
            {t("sshServers.deployKey")}
          </button>
        ) : null}
        <button type="button" onClick={() => void test()} disabled={busy || !form.id} style={secondaryButtonStyle}>
          {t("sshServers.test")}
        </button>
        <button type="button" onClick={() => void save()} disabled={busy} style={primaryButtonStyle}>
          {busy ? t("common.saving") : t("common.save")}
        </button>
      </div>

      {importOpen ? (
        <div style={{ marginTop: "12px", borderTop: "1px solid var(--border-color)", paddingTop: "10px" }}>
          <div style={{ fontSize: "12px", fontWeight: 700, marginBottom: "6px" }}>
            {importSource === "ssh_config" ? t("sshServers.importLocalTitle") : t("sshServers.importFileTitle")}
          </div>
          {importPreview?.needs_passphrase ? (
            <div style={{ display: "flex", gap: "6px", marginBottom: "8px" }}>
              <input
                value={importPassphrase}
                onChange={(event) => setImportPassphrase(event.target.value)}
                placeholder={t("sshServers.importPassphrase")}
                style={{ ...inputStyle, flex: 1 }}
                type="password"
              />
              <button
                type="button"
                disabled={busy || !importPassphrase}
                onClick={() => void runImportPreview("json", importPayload, importPassphrase)}
                style={secondaryButtonStyle}
              >
                {t("sshServers.decrypt")}
              </button>
            </div>
          ) : null}
          {importPreview ? (
            <div style={{ display: "grid", gap: "4px", maxHeight: "200px", overflowY: "auto" }}>
              {importPreview.entries.map((entry) => (
                <div key={entry.meta.alias} style={{ display: "flex", gap: "6px", alignItems: "center", fontSize: "11px" }}>
                  <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {entry.meta.alias} → {entry.meta.host}
                    {entry.has_secret ? " 🔐" : ""}
                  </span>
                  {!entry.valid ? (
                    <span style={{ color: "#ef4444" }}>{entry.error || t("sshServers.invalidEntry")}</span>
                  ) : entry.conflict ? (
                    <select
                      value={importResolutions[entry.meta.alias] || "suffix"}
                      onChange={(event) =>
                        setImportResolutions((prev) => ({ ...prev, [entry.meta.alias]: event.target.value as "overwrite" | "suffix" | "skip" }))
                      }
                      style={{ ...inputStyle, width: "auto" }}
                    >
                      <option value="suffix">{t("sshServers.resolveSuffix")}</option>
                      <option value="overwrite">{t("sshServers.resolveOverwrite")}</option>
                      <option value="skip">{t("sshServers.resolveSkip")}</option>
                    </select>
                  ) : (
                    <span style={{ color: "var(--text-secondary)" }}>{t("sshServers.willCreate")}</span>
                  )}
                </div>
              ))}
              <button type="button" onClick={() => void applyImport()} disabled={busy} style={{ ...primaryButtonStyle, marginTop: "6px" }}>
                {t("sshServers.applyImport")}
              </button>
            </div>
          ) : (
            <div style={{ fontSize: "11px", color: "var(--text-secondary)" }}>{t("sshServers.importHint")}</div>
          )}
          <button type="button" onClick={() => { setImportOpen(false); setImportPreview(null); }} style={{ ...secondaryButtonStyle, marginTop: "8px" }}>
            {t("common.cancel")}
          </button>
        </div>
      ) : null}
    </div>
  );
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  wide = false,
  type = "text",
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  wide?: boolean;
  type?: string;
}) {
  return (
    <label style={{ display: "grid", gap: "4px", gridColumn: wide ? "1 / -1" : undefined }}>
      <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} style={inputStyle} type={type} />
    </label>
  );
}

const inputStyle: React.CSSProperties = {
  border: "1px solid var(--border-color)",
  background: "var(--input-bg, transparent)",
  color: "var(--text-primary)",
  borderRadius: "8px",
  padding: "7px 8px",
  fontSize: "12px",
  outline: "none",
  minWidth: 0,
};

const iconButtonStyle: React.CSSProperties = {
  width: "24px",
  height: "24px",
  borderRadius: "8px",
  border: "1px solid var(--border-color)",
  background: "transparent",
  color: "var(--text-secondary)",
  cursor: "pointer",
};

const secondaryButtonStyle: React.CSSProperties = {
  border: "1px solid var(--border-color)",
  background: "transparent",
  color: "var(--text-primary)",
  borderRadius: "8px",
  padding: "7px 10px",
  fontSize: "12px",
  cursor: "pointer",
};

const dangerButtonStyle: React.CSSProperties = {
  border: "1px solid rgba(239,68,68,0.5)",
  background: "transparent",
  color: "#ef4444",
  borderRadius: "8px",
  padding: "7px 10px",
  fontSize: "12px",
  cursor: "pointer",
};

const primaryButtonStyle: React.CSSProperties = {
  border: "none",
  background: "var(--accent-color)",
  color: "#fff",
  borderRadius: "8px",
  padding: "7px 12px",
  fontSize: "12px",
  cursor: "pointer",
};
