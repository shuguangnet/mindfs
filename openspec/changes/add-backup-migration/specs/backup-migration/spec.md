## ADDED Requirements

### Requirement: Encrypted instance backup bundle

The system SHALL provide a one-click export that packs the global configuration directory and every registered project root's metadata directory into a single passphrase-encrypted bundle file.

#### Scenario: export with passphrase
- **WHEN** the user requests a backup with a passphrase of at least 8 characters
- **THEN** the server streams a `mindfs-backup-*.mbk` file containing a JSON header (format, version, KDF parameters, chunk size/count, payload digest) followed by AES-256-GCM encrypted chunks of a tar.gz payload with `manifest.json`, `global/…`, and `roots/<rootID>/…` entries
- **AND** the download does not require the payload to be fully buffered in memory

#### Scenario: short passphrase rejected
- **WHEN** the passphrase is shorter than 8 characters
- **THEN** export fails with a structured validation error and no bundle is produced

#### Scenario: secrets never in the clear
- **WHEN** the bundle is written
- **THEN** every file that contains credentials (API providers, SSH server store and master key, E2EE key, relay/push identity) is only present inside the encrypted payload, and the unencrypted header carries no secret material

### Requirement: Backup contents and options

The backup SHALL include the global config directory (excluding backup artifacts), each root's sessions including the `session-list.db` index, task board database, uploaded files, and file metadata, subject to user toggles.

#### Scenario: uploads toggle
- **WHEN** the user disables "include uploads" before exporting
- **THEN** the bundle omits `upload/` contents and the manifest records `include_uploads: false`

#### Scenario: device identity toggle
- **WHEN** the user disables "include device identity"
- **THEN** `device.json`, `web-push-vapid.json`, and `local-cli-tokens.json` are excluded and the manifest records it

#### Scenario: inconsistent sqlite capture degrades safely
- **WHEN** a copied database fails the consistency check during staging
- **THEN** that database is omitted, the jsonl session files still ship, and a warning is recorded in the manifest

### Requirement: Two-phase restore

The system SHALL support uploading a bundle with its passphrase to obtain a preview before any data is applied, and SHALL require an explicit apply call with per-section choices to modify local state.

#### Scenario: preview without changes
- **WHEN** a valid bundle and passphrase are uploaded
- **THEN** the server responds with the manifest summary — bundle version, creation time, MindFS version skew, per-root id/name/path/meta-location/size/count, conflict status (new, same-path, path-missing-on-disk), and global section summary — without writing any project or config data

#### Scenario: wrong passphrase
- **WHEN** the passphrase does not decrypt the bundle
- **THEN** the import fails with an authentication error and no staging side effects beyond a wiped temp dir

#### Scenario: tampered or truncated bundle
- **WHEN** header fields, chunk order, or payload bytes are modified or the file is truncated
- **THEN** decryption fails the AEAD or digest check and the import is rejected

#### Scenario: staging expiry
- **WHEN** an apply token is older than 30 minutes
- **THEN** the apply request fails and the staging directory has been removed

### Requirement: Apply with conflict handling

The apply step SHALL honor per-root actions (skip, merge, replace) with an optional path override per root, and a global-config action (skip, replace).

#### Scenario: merge preserves existing sessions
- **WHEN** a root is applied with action `merge`
- **THEN** session jsonl/aux and upload files that do not exist locally are copied, rows from the bundle's `sessions` and `session_agent_bindings` tables are inserted with conflict-skip semantics, and existing local sessions are not modified

#### Scenario: replace overwrites target metadata
- **WHEN** a root is applied with action `replace`
- **THEN** all staged files for that root overwrite the local metadata directory and per-root counters are returned

#### Scenario: path override registers a new location
- **WHEN** a root is applied with a `path_override` pointing to a directory that exists on disk
- **THEN** the root is (re)registered at that path before restore so the migrated metadata appears under the new machine's layout

#### Scenario: global replace keeps a rollback copy
- **WHEN** global config is applied with action `replace`
- **THEN** each overwritten file is first moved aside to a timestamped `.bak-` sibling with restrictive permissions, and device-identity files are skipped unless explicitly included

#### Scenario: caches refreshed after apply
- **WHEN** apply completes
- **THEN** cached per-root session managers and the SSH store are invalidated for affected sections, SSH config materialization re-runs when the SSH store was restored, and the response indicates that a service restart is recommended

### Requirement: Path traversal protection on restore

The import SHALL reject archive members that are not plain files/directories under the staging root.

#### Scenario: malicious member
- **WHEN** the bundle contains absolute paths, `..` segments, or symlink/device entries
- **THEN** the import fails with a structured error and nothing is written outside the staging directory
