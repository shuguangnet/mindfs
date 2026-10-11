# Design: add-backup-migration

## Context

- Global config lives in `~/.config/mindfs/` (agents.json, agents-config/, api-providers.json, device.json, e2ee.json, local-cli-tokens.json, preferences.json, prompts.json, registry.json, ssh-servers.json + ssh-secret.key, task_template.json, usage.json, web-push-vapid.json, probe-sessions.json). Several of these are secrets.
- Per-root metadata lives in `<root>/.mindfs/` or `~/.mindfs/<rootId>/` (chosen at registration): `sessions/*.jsonl` + `sessions/*.aux.jsonl` + `sessions/session-list.db` (sqlite, authoritative session index + `session_agent_bindings`), `tasks/task-kanban.db`, `upload/`, `file-meta.json`.
- The session manager never rescans jsonl to rebuild the index — `session-list.db` is authoritative, so it must ship in the bundle.
- Pi native sessions: `~/.pi/agent/sessions/<dir>/<file>.jsonl` where `<dir> = "--" + cwd with leading slash stripped and `/`, `\`, `:` replaced by "-" + "--"` and `<file> = ISO-timestamp-with-":"/"."→"-"_<sessionId>.jsonl`. Format v3: header `{"type":"session","version":3,"id","timestamp","cwd"}` then entries `{"type":"message","id","parentId","timestamp","message":{"role","content":[{"type":"text","text"}]}}` with an 8-hex-char id chain (parentId of the first entry is null). MindFS resumes ACP sessions by `agent_session_id`, and pi resolves resumed ids by scanning session files for that exact id.

## Goals / Non-Goals

Goals: single-file encrypted backup of all MindFS state; two-phase restore with per-root conflict choices; pi session round-trip via native export. Non-goals: source-tree backup, scheduled backups, kanban merge, non-pi native exporters, cross-agent format translation.

## Bundle format (v1)

File `mindfs-backup-YYYYMMDD-HHMMSS.mbk`:

```
line 1: JSON header + '\n'
  {"format":"mindfs-backup","version":1,"created_at":RFC3339,
   "mindfs_version":"…","kdf":{"salt_b64","iterations":600000},
   "nonce_prefix_b64":"4 bytes","chunk_size":262144,"chunks":N,
   "payload_sha256":"hex of decrypted payload"}
bytes: N concatenated AES-256-GCM records (ciphertext || 16-byte tag),
  each decrypting to exactly chunk_size plaintext except the LAST chunk
  which is always a 32-byte SHA-256 marker equal to payload_sha256.
```

- Key: PBKDF2-SHA256(passphrase, salt, iterations from header, 32 bytes) — same KDF family as the SSH export envelope so policies stay aligned.
- Nonce per chunk: `nonce_prefix || 8-byte big-endian counter`; counter starts at 1. Uniqueness is guaranteed by construction.
- AAD for every chunk: `"mindfs-backup-v1|" + hex(sha256(header line without newline))` — header fields (KDF params, chunk count, digest) cannot be tampered with undetected; the final marker chunk makes truncation and chunk-reordering detectable.
- Payload: `tar.gz` stream:
  - `manifest.json`
  - `global/<relative-path>` — regular files under the config dir (skip `*.bak-*`, temp/import staging).
  - `roots/<rootID>/…` — the root's effective metadata directory contents.

Streaming both directions (io.Writer/io.Reader); nothing large is buffered in memory.

## Manifest

```json
{
  "format":"mindfs-backup","version":1,"created_at":"…","mindfs_version":"…",
  "options":{"include_uploads":true,"include_device_identity":true},
  "roots":[{"id","name","root_path","meta_location":"project"|"home",
            "file_count","bytes","has_tasks"}],
  "global":{"file_count","bytes","device_identity_included":true}
}
```

Device identity = {device.json, web-push-vapid.json, local-cli-tokens.json}; it is excluded at export when the toggle is off. `e2ee.json` always ships (inside the encrypted bundle) — without it previously E2EE-encrypted payloads stay unreadable.

## Consistent sqlite capture

For each `*.db`: copy `file`, `file-wal`, `file-shm` if present, then open the copy read-only and run `PRAGMA quick_check`; on failure record a warning in the manifest and skip that db (jsonl still ships). Restores overwrite all three sidecar files.

## Export flow

`POST /api/backup/export {passphrase, include_uploads, include_device_identity}` →
1. passphrase ≥ 8 chars (structured error otherwise).
2. Enumerate roots from the live registry; for each, walk the effective meta dir (skip uploads when disabled; cap single-file size at 512 MiB with a warning).
3. Stream tar.gz → chunked AEAD → HTTP response body (`application/octet-stream`, `Content-Disposition: attachment; filename=…`). The header line is written first with `chunks`/`payload_sha256` computed during streaming: implemented by writing payload to a temp file while sealing, then rewriting the header line (fixed-width reserved zero-padded numeric fields keep the header a stable single line — final header is rewritten before the file is served; the on-disk temp file is fully seekable so this is a two-pass operation).

## Import flow

**Phase 1 — `POST /api/backup/import`** (multipart `file` + `passphrase`):
1. Read header, validate format/version, derive key, stream-decrypt to `<tmp>/mindfs-import-<token>/payload.tar.gz`, verifying chunk integrity and the trailing digest marker.
2. Extract tar.gz into the staging dir with path traversal protection (reject `..`, absolute paths, symlinks).
3. Parse `manifest.json`, compare root paths against the live registry and filesystem → conflict status (`exists_same_path`, `path_missing_on_disk`, `new`), detect version skew.
4. Keep the staging dir server-side keyed by an opaque import token (random 128-bit); lazy 30-minute expiry (checked on apply; background sweeper optional).
5. Respond with the preview (no file contents).

**Phase 2 — `POST /api/backup/apply {token, roots:[{id, action, path_override}], global, include_device_identity}`**:
- Per root:
  - `path_override` (non-empty) → register the root at the new path (`registry.UpsertWithMetaLocation`) and target its meta dir.
  - `merge` (default for existing roots): copy `sessions/*.jsonl`, `*.aux.jsonl`, and upload files that do not exist yet; ATTACH the staged sqlite db and `INSERT OR IGNORE` rows from `sessions` and `session_agent_bindings` (both keyed, conflicts skipped); kanban db is never merged (warning if staged root has tasks and a local one exists).
  - `replace`: overwrite every staged file into the meta dir (extra local files are kept); effective when the target is fresh or the user explicitly chooses it.
- Global `replace`: for each staged file under `global/`, move the current file aside as `<name>.bak-<ts>` (0600) and write the new one; device-identity files are skipped unless `include_device_identity`. Global `skip` leaves everything untouched.
- After apply: invalidate cached session managers / kanban / ssh store for affected roots so reopened handles see the new files, and re-run SSH config materialization when `ssh-servers.json` was restored. Response carries per-root counters (sessions merged, files copied), global counters, warnings, and `reload: true` (UI advises a service restart for agent pools).

## Native pi export

`POST /api/roots/{rootID}/sessions/{key}/export-native {mode:"write"|"download"}` (pi only in v1):
1. Load exchanges via the session manager (`Get(key, 0)`); map user/assistant text exchanges to pi message entries (tool calls are summarized in v1 as text blocks prefixed `[tool]` when their result is textual; binary/image aux is skipped).
2. Session id = binding's `agent_session_id` when a pi binding exists (keeps MindFS↔pi resume linked), else a generated UUIDv7 (timestamp-ms + random per RFC 9562 layout, matching pi's id charset).
3. `write`: emit `~/.pi/agent/sessions/<munged-cwd>/<ts>_<id>.jsonl` (0600, atomic create, idempotent — an existing file with the same id is left untouched and reported). `download`: return the generated jsonl as an attachment for manual placement.
4. The mapping layer (`server/internal/piexport`) is agent-agnostic in shape so future exporters (claude/codex "download-only") slot in beside it.

## Security

- Bundle secrets are only ever protected by the passphrase (PBKDF2 600k + AES-256-GCM per chunk + digest marker); export/import responses never echo file contents; import staging lives under the OS temp dir with 0700 dirs and is wiped on expiry/apply.
- Import rejects unexpected member types (symlinks, devices), absolute paths, and `..` segments; tar entries are size-capped.
- API endpoints are protected like all other management routes; export requires an explicit passphrase (no plaintext fallback).

## Risks / Trade-offs

- Live dbs are copied as files; the quick_check guard catches torn copies and falls back to jsonl-only + warning. Acceptable because sessions append-only and merges use `INSERT OR IGNORE`.
- Restoring `device.json`/relay identity while the old instance still runs may cause relay conflicts; the UI labels the toggle accordingly ("整机迁移时勾选").
- Replace for global config changes running behavior only after restart (agent pool env is cached); the apply response says so.
