import React from "react";
import {
  deleteRemoteServer,
  fetchRemoteServers,
  saveRemoteServer,
  testRemoteServer,
  type RemoteRoot,
  type RemoteServer,
} from "../services/remoteServers";

type Props = {
  open: boolean;
  onClose: () => void;
};

const emptyServer: RemoteServer = {
  id: "",
  name: "",
  base_url: "",
  node_id: "",
  pairing_secret: "",
  default_root_id: "",
  enabled: true,
};

export function RemoteServersDialog({ open, onClose }: Props) {
  const [servers, setServers] = React.useState<RemoteServer[]>([]);
  const [form, setForm] = React.useState<RemoteServer>(emptyServer);
  const [roots, setRoots] = React.useState<RemoteRoot[]>([]);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [summary, setSummary] = React.useState("");

  const load = React.useCallback(async () => {
    setBusy(true);
    setError("");
    try {
      const items = await fetchRemoteServers();
      setServers(items);
      if (!form.id && items.length > 0) {
        setForm({ ...items[0], pairing_secret: "" });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载远端服务器失败");
    } finally {
      setBusy(false);
    }
  }, [form.id]);

  React.useEffect(() => {
    if (open) {
      void load();
    }
  }, [load, open]);

  if (!open) return null;

  const update = (patch: Partial<RemoteServer>) => {
    setForm((prev) => ({ ...prev, ...patch }));
    setSummary("");
    setError("");
  };

  const selectServer = (server: RemoteServer) => {
    setForm({ ...server, pairing_secret: "" });
    setRoots([]);
    setSummary("");
    setError("");
  };

  const save = async () => {
    setBusy(true);
    setError("");
    try {
      const saved = await saveRemoteServer(form);
      setForm({ ...saved, pairing_secret: "" });
      setSummary("已保存");
      const items = await fetchRemoteServers();
      setServers(items);
      if (typeof window !== "undefined") {
        window.dispatchEvent(new Event("mindfs-agents-changed"));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存失败");
    } finally {
      setBusy(false);
    }
  };

  const test = async () => {
    if (!form.id) {
      setError("请先保存服务器");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const result = await testRemoteServer(form.id);
      setRoots(result.roots || []);
      setSummary(`连接正常 · Agent ${result.agents?.length || 0} · Shell ${result.shells?.length || 0}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "测试失败");
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!form.id) return;
    setBusy(true);
    setError("");
    try {
      await deleteRemoteServer(form.id);
      setForm(emptyServer);
      setRoots([]);
      setSummary("已删除");
      setServers(await fetchRemoteServers());
      if (typeof window !== "undefined") {
        window.dispatchEvent(new Event("mindfs-agents-changed"));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除失败");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      style={{
        border: "1px solid var(--border-color)",
        background: "var(--menu-bg)",
        borderRadius: "10px",
        boxShadow: "0 18px 45px rgba(15, 23, 42, 0.18)",
        padding: "12px",
        color: "var(--text-primary)",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "10px" }}>
        <div style={{ fontSize: "13px", fontWeight: 700, flex: 1 }}>远端服务器</div>
        <button type="button" onClick={() => { setForm(emptyServer); setRoots([]); }} style={iconButtonStyle} aria-label="新建远端服务器">
          +
        </button>
        <button type="button" onClick={onClose} style={iconButtonStyle} aria-label="关闭远端服务器">
          x
        </button>
      </div>

      {servers.length > 0 ? (
        <div style={{ display: "flex", gap: "6px", overflowX: "auto", marginBottom: "10px" }}>
          {servers.map((server) => (
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
              }}
            >
              {server.name || server.id}
            </button>
          ))}
        </div>
      ) : null}

      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "8px" }}>
        <Field label="ID" value={form.id} onChange={(value) => update({ id: value })} placeholder="prod-a" />
        <Field label="名称" value={form.name} onChange={(value) => update({ name: value })} placeholder="生产机" />
        <Field label="Base URL" value={form.base_url} onChange={(value) => update({ base_url: value })} placeholder="https://..." wide />
        <Field label="Node ID" value={form.node_id} onChange={(value) => update({ node_id: value })} placeholder="远端 node_id" />
        <Field label="Pairing Secret" value={form.pairing_secret || ""} onChange={(value) => update({ pairing_secret: value })} placeholder={form.has_secret ? "留空保留原 secret" : "必填"} />
        <Field label="默认 Root ID" value={form.default_root_id} onChange={(value) => update({ default_root_id: value })} placeholder="mindfs" />
      </div>

      {roots.length > 0 ? (
        <select
          value={form.default_root_id}
          onChange={(event) => update({ default_root_id: event.target.value })}
          style={{ ...inputStyle, width: "100%", marginTop: "8px" }}
        >
          {roots.map((root) => (
            <option key={root.id} value={root.id}>
              {root.display_name || root.name || root.id}
            </option>
          ))}
        </select>
      ) : null}

      <label style={{ display: "flex", gap: "8px", alignItems: "center", fontSize: "12px", marginTop: "10px" }}>
        <input type="checkbox" checked={form.enabled} onChange={(event) => update({ enabled: event.target.checked })} />
        启用
      </label>

      {error ? <div style={{ color: "#ef4444", fontSize: "12px", marginTop: "8px" }}>{error}</div> : null}
      {summary ? <div style={{ color: "var(--text-secondary)", fontSize: "12px", marginTop: "8px" }}>{summary}</div> : null}

      <div style={{ display: "flex", gap: "8px", justifyContent: "flex-end", marginTop: "12px" }}>
        {form.id ? (
          <button type="button" onClick={remove} disabled={busy} style={secondaryButtonStyle}>
            删除
          </button>
        ) : null}
        <button type="button" onClick={test} disabled={busy || !form.id} style={secondaryButtonStyle}>
          测试
        </button>
        <button type="button" onClick={save} disabled={busy} style={primaryButtonStyle}>
          保存
        </button>
      </div>
    </div>
  );
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  wide = false,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  wide?: boolean;
}) {
  return (
    <label style={{ display: "grid", gap: "4px", gridColumn: wide ? "1 / -1" : undefined }}>
      <span style={{ fontSize: "11px", color: "var(--text-secondary)" }}>{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} style={inputStyle} />
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

const primaryButtonStyle: React.CSSProperties = {
  border: "none",
  background: "var(--accent-color)",
  color: "#fff",
  borderRadius: "8px",
  padding: "7px 12px",
  fontSize: "12px",
  cursor: "pointer",
};
