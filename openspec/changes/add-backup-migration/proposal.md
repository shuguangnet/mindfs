# Proposal: add-backup-migration

## Why

MindFS positions itself as "self-hosted data" (sessions, tasks, credentials all live on the user's machine), but self-hosters currently have no safe way to move an instance: rebuilding a server, migrating hosts, or simply guarding against disk failure all require hand-copying `~/.config/mindfs/`, every project's `.mindfs/` (or `~/.mindfs/<rootId>/`), plus the SSH credentials store — with no encryption in transit and no conflict handling. Additionally, after migrating only the MindFS data, ACP-based agents (pi in particular) lose their native session files, so neither bare `pi resume` nor MindFS resume can continue past conversations even though the exchanges were backed up.

## What Changes

- Add an **encrypted full-instance backup**: one passphrase-protected bundle file containing the global config directory (agents, API providers, preferences, prompts, registry, SSH server credentials + master key, E2EE key, usage, task templates) and every project root's metadata (sessions with indexes, task board, uploads, file metadata).
- Add **two-phase restore**: upload + passphrase → server-side preview (roots with conflict status, sizes, counts, warnings) → apply with per-root actions (`skip` / `merge` / `replace`, optional path override for new machines) and a global-config action.
- Add **native session export for pi**: write a MindFS session (or download it) as a native pi session file under `~/.pi/agent/sessions/<munged-cwd>/`, reusing the existing agent binding's session id when present so both MindFS resume and bare `pi resume` keep working after migration.
- Sidebar entry "备份与迁移" with export and import cards; per-session "导出到原生 pi" action for pi sessions.

## Capabilities

- `backup-migration` — bundle format, export, preview, apply, conflict handling, security requirements.
- `native-session-export` — MindFS session → native pi session file (write or download), id linkage.

## Impact

- **New packages**: `server/internal/backupbundle` (chunked passphrase AEAD stream), `server/internal/backup` (collect/export/import/apply), `server/internal/piexport` (pi session writer).
- **API**: `POST /api/backup/export` (binary stream), `POST /api/backup/import` (multipart + passphrase → preview), `POST /api/backup/apply`, `POST /api/roots/{rootID}/sessions/{key}/export-native`; all protected.
- **Existing code**: route table, appcontext wiring, session-manager cache invalidation after apply, web UI (sidebar menu, dialog, session action menu), i18n.
- **Out of scope (v1)**: backing up project source trees, scheduling automatic backups, task-board (kanban) merging (replace only), exporting native sessions for claude/codex (they already persist native files via their SDKs).
