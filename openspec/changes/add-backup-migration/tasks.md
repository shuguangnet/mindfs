# Tasks: add-backup-migration

## 1. Bundle crypto (`server/internal/backupbundle`)

- [x] 1.1 Implement the v1 bundle writer: JSON header (fixed-width `chunks`/`payload_sha256` fields for stable rewrite), PBKDF2-SHA256 key derivation, per-chunk AES-256-GCM with `nonce_prefix || counter` nonces, header-hash AAD, and a final 32-byte digest marker chunk; stream seal from an `io.Reader` into a seekable temp file and rewrite the header afterwards.
- [x] 1.2 Implement the reader: parse/validate header, stream open with the same nonce/AAD scheme, enforce chunk size and final marker equality with the header digest.
- [x] 1.3 Tests: round trip across chunk boundaries, wrong passphrase, tampered header byte, flipped ciphertext byte, truncated stream, reordered chunks.

## 2. Backup collection and export (`server/internal/backup`)

- [x] 2.1 Implement manifest model and root/global enumeration from the live registry, honoring `include_uploads` and `include_device_identity` toggles and recording warnings.
- [x] 2.2 Implement the tar.gz payload builder: `manifest.json`, `global/…` (skip `*.bak-*`), `roots/<id>/…`, 512 MiB per-file cap with warning, sqlite quick_check guard with graceful skip, and `upload/` exclusion toggle.
- [x] 2.3 Implement `Export`: two-pass seal into a temp file, 0600 staging cleanup, suggested filename `mindfs-backup-YYYYMMDD-HHMMSS.mbk`, passphrase ≥ 8 validation with structured errors.
- [x] 2.4 Tests: export contains expected members and options; sqlite guard path; toggles reflected in manifest.

## 3. Import preview and apply

- [x] 3.1 Implement staged import: multipart upload → stream decrypt to `<tmp>/mindfs-import-<token>/payload.tar.gz`, safe extraction (no symlink/device/absolute/`..` members, size caps), manifest parse, 30-minute lazy expiry.
- [x] 3.2 Implement preview assembly: root conflict status (new / same-path / path-missing-on-disk), version skew, global summary, warnings — no state changes.
- [x] 3.3 Implement apply: per-root `merge` (missing jsonl/aux/upload copy + `INSERT OR IGNORE` from attached sqlite for `sessions` and `session_agent_bindings`), `replace`, `path_override` re-registration, global `replace` with `.bak-<ts>` rollover and device-identity gating, kanban never merged.
- [x] 3.4 Implement post-apply invalidation hooks (session manager cache, SSH store + rematerialize when restored) exposed as an interface so the API layer wires appcontext.
- [x] 3.5 Tests: merge idempotency and conflict-skip, replace overwrite, traversal rejection, expiry, global rollover, apply counters.

## 4. Native pi export (`server/internal/piexport`)

- [x] 4.1 Implement pi session writer: cwd munging rule, session id selection (binding id or UUIDv7), v3 header, 8-hex parent-id message chain, model field mapping, `[tool]` text summaries for textual aux, 0600 atomic write, idempotency against existing ids.
- [x] 4.2 Tests: generated file parses as valid pi JSONL (structural assertions), munged dir naming against real-world samples, id reuse from binding, idempotent re-export.

## 5. API layer

- [x] 5.1 Register protected routes: `POST /api/backup/export` (octet-stream response), `POST /api/backup/import` (multipart), `POST /api/backup/apply`, `POST /api/roots/{rootID}/sessions/{key}/export-native`.
- [x] 5.2 Handlers with structured errors (validation, bad passphrase, bad bundle, expired token, unsupported agent) and no secret echoing; wire backup manager + invalidation into appcontext/server startup.
- [x] 5.3 API tests: happy-path export→import→apply round trip against a temp registry, wrong passphrase 4xx, traversal 4xx, pi export write + download modes.

## 6. Web UI

- [x] 6.1 `services/backup.ts` client (binary download via fetch→blob, multipart upload, apply).
- [x] 6.2 `BackupDialog.tsx`: export card (passphrase, toggles, download), import card (file + passphrase → preview table with per-root action/path-override/global action → apply → result summary + restart advice).
- [x] 6.3 Sidebar menu entry "备份与迁移" alongside SSH servers; per-session "导出到原生 pi" action for pi sessions with result toast.
- [x] 6.4 zh-CN / en-US strings; `tsc`, unit tests, build pass.

## 7. Verification

- [x] 7.1 `go build ./...`, `gofmt`, targeted `go test` for new packages and api.
- [ ] 7.2 End-to-end smoke: export on a scratch instance → import into a fresh config dir → merge/replace assertions → pi export resumable check (`pi` lists the exported session for the matching cwd).
- [ ] 7.3 `openspec validate add-backup-migration`; deploy locally for user testing.
