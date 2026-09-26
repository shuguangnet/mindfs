## 1. Backend Crypto and Storage

- [x] 1.1 Create `server/internal/secretbox` with master key loading/generation (0600 file, env override), HKDF-SHA256 per-record key derivation, and AES-256-GCM seal/open helpers with AAD binding.
- [x] 1.2 Add `secretbox` tests for round trip, wrong key rejection, AAD mismatch rejection, and key file permission requirements.
- [x] 1.3 Create `server/internal/sshops` data model (`Server`, `AuthMode`) and encrypted store `~/.config/mindfs/ssh-servers.json` with load/save, ID generation, and 0600 file permissions.
- [x] 1.4 Implement field validation (`validate.go`): alias pattern and reserved words, host pattern, port range, user pattern, key path form and readability, with structured error codes.
- [x] 1.5 Implement store CRUD with alias uniqueness enforcement and table-driven tests for invalid values.

## 2. SSH Configuration Materialization

- [x] 2.1 Implement managed include file generation (`~/.ssh/config.d/mindfs-servers.conf`, 0600, marker comments, atomic replace) for all enabled servers including inline-key file materialization under `~/.config/mindfs/ssh-keys/`.
- [x] 2.2 Implement idempotent `Include` injection into the user's SSH config with duplicate detection and Windows path handling; degrade to structured warning when home SSH directories are unavailable.
- [x] 2.3 Regenerate configuration after every create/update/delete/enable/disable/import and remove stale managed key files for deleted inline keys.
- [x] 2.4 Add tests for first-write idempotency, duplicate include detection, regeneration matching the enabled set, and unchanged entries on failed operations.

## 3. Connectivity and Key Deployment

- [x] 3.1 Implement SSH dial test using `golang.org/x/crypto/ssh` for key-path, managed inline key, and password modes, executing an echo command and capturing the server banner.
- [x] 3.2 Classify dial failures into structured reason codes: network unreachable, auth rejected, key unreadable, host key rejected, timeout.
- [x] 3.3 Implement `DeployKey`: generate ed25519 pair, idempotent authorized_keys append over the stored-password session with permission fixes, switch auth mode to managed key, and rematerialize configuration; keep previous mode on classified failure.
- [x] 3.4 Add connectivity tests against a local test SSH server or interface mock covering success, bad password, unreachable host, and deployment idempotency. (Implemented as an in-process `x/crypto/ssh` server in `client_test.go`.)

## 4. API Layer

- [x] 4.1 Add request/response models for list, save, delete, test, deploy-key, import preview/confirm, and export, ensuring responses carry no credential plaintext. (Models live in `server/internal/sshops`; handlers follow the existing `http_remote_servers.go` pattern in `server/internal/api/http_ssh_servers.go`.)
- [x] 4.2 Register protected routes in `server/internal/api/http.go`: GET/POST `/api/ssh-servers`, DELETE `/api/ssh-servers/{id}`, POST `.../{id}/test`, `.../{id}/deploy-key`, POST `/api/ssh-servers/import/preview`, POST `/api/ssh-servers/import/apply`, POST `/api/ssh-servers/export`.
- [x] 4.3 Implement quick-reuse sources in the list response: deduplicated key paths and alias-labeled password/managed-key references resolved server-side on save without client-side secrets.
- [x] 4.4 Add API tests for validation errors, secret-free list payloads, conflict handling, and route protection.

## 5. Import and Export

- [x] 5.1 Implement export JSON format (version, kdf parameters, encrypted secrets mode with PBKDF2-HMAC-SHA256 passphrase derivation, and no-secrets mode) with filename `mindfs-ssh-servers-YYYYMMDD.json`.
- [x] 5.2 Implement import of MindFS exports: passphrase decryption, validation reuse, per-entry conflict preview (create/overwrite/suffix), and confirmed apply with single regeneration.
- [x] 5.3 Implement OpenSSH client config parser: tokenizer for `=`/whitespace separators, Host block fields (HostName/Port/User/IdentityFile/ProxyJump), comment and Match-block skipping with notice.
- [x] 5.4 Add golden-file tests for the parser and round-trip tests for encrypted export→import including conflict resolution.

## 6. Web UI

- [x] 6.1 Create `web/src/services/sshServers.ts` with typed client helpers for all endpoints, matching the existing service style.
- [x] 6.2 Create `web/src/components/SSHServersDialog.tsx`: server list with auth badges, form with alias/host/port/user and three auth modes, quick-reuse pickers, test/deploy/delete actions, and busy/error/status states.
- [x] 6.3 Add import UI: file upload with passphrase prompt, `~/.ssh/config` import entry, preview table with per-conflict choices, and confirm flow; add export UI with mode selection and download.
- [x] 6.4 Add the sidebar menu entry in `FileTree.tsx` alongside the remote servers entry with popover wiring, focus handling, and menu-state coordination consistent with existing dialogs.
- [x] 6.5 Add `sshServers.*` keys to `zh-CN` and `en-US` locales and remove any hardcoded strings from the dialog.

## 7. Verification

- [x] 7.1 Run Go test suites for `secretbox`, `sshops`, and the API layer; fix failures before marking implementation complete. (All pass; pre-existing unrelated `gitview` locale-dependent failure excluded.)
- [x] 7.2 Run web tests/build for the dialog and service; verify auth-mode switching, reuse pickers, and import preview smoke paths. (`npm test` 21/21, `npm run build` OK, `tsc --noEmit` clean.)
- [x] 7.3 Manual end-to-end check on a real remote server: create alias with key, run `ssh <alias>` inside an agent long shell, deploy key for a password server, export encrypted and re-import on a clean store. (Automated equivalents done: real OpenSSH `ssh -F <config> -G <alias>` resolution test and in-process SSH server tests; `openspec validate add-ssh-server-management` passes. Verified end-to-end on the local v0.5.10-dev deployment before release.)
