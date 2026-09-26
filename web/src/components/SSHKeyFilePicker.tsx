import React from "react";
import { useI18n } from "../i18n";
import { browseSSHKeyFS, type SSHFSEntry, type SSHFSListing } from "../services/sshServers";

type Props = {
  initialPath?: string;
  onSelect: (path: string) => void;
  onClose: () => void;
};

function formatSize(size?: number): string {
  if (!size || size <= 0) return "";
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

// SSHKeyFilePicker browses the host filesystem (one directory level per
// request) so the user can pick a private key file instead of typing a path.
export function SSHKeyFilePicker({ initialPath, onSelect, onClose }: Props) {
  const { t } = useI18n();
  const [listing, setListing] = React.useState<SSHFSListing | null>(null);
  const [pathInput, setPathInput] = React.useState(initialPath || "");
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState("");
  const [showHidden, setShowHidden] = React.useState(true);

  const shorten = React.useCallback(
    (path: string) => {
      const home = listing?.home || "";
      if (home && path === home) return "~";
      if (home && path.startsWith(`${home}/`)) return `~${path.slice(home.length)}`;
      return path;
    },
    [listing?.home],
  );

  const load = React.useCallback(
    async (path: string) => {
      setLoading(true);
      setError("");
      try {
        const next = await browseSSHKeyFS(path);
        setListing(next);
        setPathInput(next.path);
      } catch (err) {
        setError(err instanceof Error ? err.message : t("sshServers.fsListFailed"));
      } finally {
        setLoading(false);
      }
    },
    [t],
  );

  React.useEffect(() => {
    void load(initialPath || "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const pickEntry = async (entry: SSHFSEntry) => {
    if (entry.is_dir) {
      await load(entry.path);
      return;
    }
    if (entry.is_link) {
      // A symlink may point at a directory; try to navigate first and fall
      // back to selecting it as a file when it is not a directory.
      try {
        const next = await browseSSHKeyFS(entry.path);
        setListing(next);
        setPathInput(next.path);
        return;
      } catch {
        onSelect(shorten(entry.path));
        return;
      }
    }
    onSelect(shorten(entry.path));
  };

  const entries = (listing?.entries ?? []).filter((entry) => showHidden || !entry.name.startsWith("."));

  const quickLink = (label: string, path: string, title?: string) => (
    <button
      key={path}
      type="button"
      onClick={() => void load(path)}
      disabled={loading}
      style={quickLinkStyle}
      title={title || label}
    >
      {label}
    </button>
  );

  return (
    <div
      style={{
        border: "1px solid var(--border-color)",
        borderRadius: "8px",
        padding: "8px",
        display: "grid",
        gap: "6px",
        background: "var(--menu-bg)",
      }}
    >
      <div style={{ fontSize: "12px", fontWeight: 700 }}>{t("sshServers.fsPickTitle")}</div>
      <div style={{ display: "flex", gap: "6px" }}>
        <input
          value={pathInput}
          onChange={(event) => setPathInput(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              void load(pathInput.trim());
            }
          }}
          placeholder="/absolute/path or ~/..."
          spellCheck={false}
          style={{ ...inputStyle, flex: 1, fontFamily: "monospace" }}
        />
        <button type="button" onClick={() => void load(pathInput.trim())} disabled={loading} style={smallButtonStyle}>
          {t("sshServers.fsGo")}
        </button>
      </div>
      <div style={{ display: "flex", gap: "6px", flexWrap: "wrap", alignItems: "center" }}>
        {listing?.parent ? (
          <button type="button" onClick={() => void load(listing.parent || "/")} disabled={loading} style={quickLinkStyle} title={listing.parent}>
            ↑
          </button>
        ) : null}
        {quickLink(t("sshServers.fsHome"), "~")}
        {quickLink("~/.ssh", "~/.ssh")}
        {quickLink(t("sshServers.fsRoot"), "/")}
        <label style={{ display: "flex", gap: "4px", alignItems: "center", fontSize: "11px", marginLeft: "auto", cursor: "pointer" }}>
          <input type="checkbox" checked={showHidden} onChange={(event) => setShowHidden(event.target.checked)} />
          {t("sshServers.fsShowHidden")}
        </label>
      </div>

      {error ? <div style={{ color: "#ef4444", fontSize: "11px", wordBreak: "break-all" }}>{error}</div> : null}

      <div style={{ maxHeight: "220px", overflowY: "auto", border: "1px solid var(--border-color)", borderRadius: "8px" }}>
        {entries.length === 0 ? (
          <div style={{ padding: "10px", fontSize: "11px", color: "var(--text-secondary)" }}>
            {loading ? t("sshServers.fsLoading") : t("sshServers.fsEmpty")}
          </div>
        ) : (
          entries.map((entry) => (
            <button
              key={entry.path}
              type="button"
              onClick={() => void pickEntry(entry)}
              disabled={loading}
              title={entry.path}
              style={entry.is_dir ? dirRowStyle : entry.looks_like_key ? keyRowStyle : fileRowStyle}
            >
              <span style={{ marginRight: "6px" }}>{entry.is_dir ? "📁" : entry.looks_like_key ? "🔑" : "📄"}</span>
              <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", textAlign: "left" }}>
                {entry.name}
                {entry.is_link ? " →" : ""}
              </span>
              <span style={{ color: "var(--text-secondary)", fontSize: "10px", whiteSpace: "nowrap" }}>{formatSize(entry.size)}</span>
            </button>
          ))
        )}
      </div>

      {listing?.truncated ? (
        <div style={{ color: "#d97706", fontSize: "11px" }}>{t("sshServers.fsTruncated")}</div>
      ) : null}
      <div style={{ fontSize: "10px", color: "var(--text-secondary)" }}>{t("sshServers.fsHint")}</div>
      <div style={{ display: "flex", justifyContent: "flex-end" }}>
        <button type="button" onClick={onClose} style={smallButtonStyle}>
          {t("common.cancel")}
        </button>
      </div>
    </div>
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

const quickLinkStyle: React.CSSProperties = {
  border: "1px solid var(--border-color)",
  background: "transparent",
  color: "var(--text-primary)",
  borderRadius: "8px",
  padding: "4px 8px",
  fontSize: "11px",
  cursor: "pointer",
};

const smallButtonStyle: React.CSSProperties = {
  border: "1px solid var(--border-color)",
  background: "transparent",
  color: "var(--text-primary)",
  borderRadius: "8px",
  padding: "5px 10px",
  fontSize: "11px",
  cursor: "pointer",
};

const baseRowStyle: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: "4px",
  width: "100%",
  border: "none",
  background: "transparent",
  color: "var(--text-primary)",
  padding: "5px 8px",
  fontSize: "12px",
  cursor: "pointer",
};

const dirRowStyle: React.CSSProperties = { ...baseRowStyle, fontWeight: 600 };

const keyRowStyle: React.CSSProperties = { ...baseRowStyle, background: "rgba(59,130,246,0.08)" };

const fileRowStyle: React.CSSProperties = baseRowStyle;
