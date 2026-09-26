## ADDED Requirements

### Requirement: MindFS SHALL store SSH server definitions with encrypted credentials
The system SHALL provide managed SSH server entries containing alias, host, port, user, and authentication mode, and SHALL persist credential material (passwords and inline key contents) only in encrypted form while never returning credential plaintext through any API response.

#### Scenario: Create a server with a password
- **WHEN** the user saves an SSH server with authentication mode password
- **THEN** the system stores the password encrypted with an AES-256-GCM envelope keyed from a local master key file with 0600 permissions and returns only metadata plus a `has_password` indicator

#### Scenario: Create a server with an inline key
- **WHEN** the user pastes private key content instead of choosing a path
- **THEN** the system stores the key content encrypted, materializes it as a MindFS-managed key file with 0600 permissions, and references that file path in the generated SSH configuration

#### Scenario: List servers
- **WHEN** the user requests the SSH server list
- **THEN** the system returns alias, host, port, user, authentication mode, key path when a filesystem path is used, and secret-presence flags without any password or key content

#### Scenario: Master key is missing or unreadable
- **WHEN** the master key file cannot be created or read
- **THEN** the system returns a structured error explaining the credential storage problem and refuses to persist credential material rather than falling back to plaintext

### Requirement: MindFS SHALL validate SSH server fields against injection
The system SHALL validate alias, host, port, user, and key path values before persisting or materializing them, rejecting values that could inject additional directives into OpenSSH configuration.

#### Scenario: Alias with invalid characters
- **WHEN** the user saves a server whose alias contains whitespace, newlines, `=`, `#`, or exceeds 64 characters
- **THEN** the system rejects the save with a structured validation error and does not write any SSH configuration

#### Scenario: Host contains configuration metacharacters
- **WHEN** the user saves a server whose host contains whitespace, newline, `=`, or comment characters
- **THEN** the system rejects the save with a structured validation error

#### Scenario: Duplicate alias
- **WHEN** the user saves a server with an alias already used by another entry
- **THEN** the system rejects the save with a conflict error unless the save updates the same entry

#### Scenario: Key path is not usable
- **WHEN** the user saves a key-path server whose path is not an absolute or home-relative path or cannot be read
- **THEN** the system returns a structured error identifying the key path problem

### Requirement: MindFS SHALL materialize aliases into OpenSSH-compatible configuration
The system SHALL write all enabled SSH servers into a MindFS-managed include file under the user's SSH configuration directory and SHALL idempotently ensure the user's SSH config includes it, so that `ssh <alias>` resolves for agent shells, terminals, and local ssh invocations.

#### Scenario: First server saved
- **WHEN** the user saves the first enabled SSH server
- **THEN** the system creates the managed include file with 0600 permissions containing a `Host <alias>` block with HostName, Port, User, and IdentityFile when key authentication applies, and adds a non-duplicated include directive to the user's SSH config

#### Scenario: Configuration regenerated after changes
- **WHEN** a server is created, updated, enabled, disabled, deleted, or imported
- **THEN** the system regenerates the managed include file so its content matches the current enabled server set exactly, using atomic file replacement

#### Scenario: Existing include directive
- **WHEN** the user's SSH config already contains an equivalent include directive
- **THEN** the system does not insert a duplicate directive

#### Scenario: SSH directory cannot be created
- **WHEN** the user's home or SSH configuration directory cannot be created or written
- **THEN** the system returns a structured warning while keeping the encrypted server entry stored

#### Scenario: Alias resolves in an agent shell
- **WHEN** an agent executes `ssh <alias>` in a MindFS long shell after the alias has been materialized with key authentication
- **THEN** the operating system ssh client resolves the alias through the managed include file and connects without further configuration

### Requirement: MindFS SHALL support quick reuse of credentials
The system SHALL let the user reuse an existing key path or an existing stored password when configuring another server, without re-entering or revealing the secret.

#### Scenario: Reuse a key path
- **WHEN** the user opens the key path quick-reuse picker
- **THEN** the system offers the deduplicated list of key paths already used by other servers and fills the field on selection

#### Scenario: Reuse a password
- **WHEN** the user opens the password quick-reuse picker
- **THEN** the system offers other servers that have a stored password labeled by alias, and saving copies the referenced secret server-side without exposing it to the client

#### Scenario: Reuse a managed key
- **WHEN** the user chooses to reuse the managed key of another server
- **THEN** the new server references the same managed key material without duplicating or displaying private key content

### Requirement: MindFS SHALL import SSH server configurations
The system SHALL import SSH servers from a MindFS export file and from the local OpenSSH client configuration, with a preview step before any entry is written.

#### Scenario: Import from MindFS export with passphrase
- **WHEN** the user imports an encrypted export file and provides the correct passphrase
- **THEN** the system decrypts the entries, validates them with the same rules as manual creation, and shows a preview of entries to create and alias conflicts before confirming

#### Scenario: Import from OpenSSH client config
- **WHEN** the user imports the local `~/.ssh/config`
- **THEN** the system parses `Host` blocks including HostName, Port, User, IdentityFile, and ProxyJump values, skips `Match` blocks with a notice, maps existing IdentityFile entries to key-path mode, and previews entries before confirming

#### Scenario: Alias conflicts during import
- **WHEN** imported entries collide with existing aliases
- **THEN** the preview marks each conflict and the user chooses per-entry overwrite or suffix rename

### Requirement: MindFS SHALL export SSH server configurations
The system SHALL export SSH servers to a portable JSON file with credential material protected by a passphrase by default.

#### Scenario: Encrypted export
- **WHEN** the user exports with a passphrase
- **THEN** the system returns a JSON file whose secrets are encrypted with a passphrase-derived key using the parameters embedded in the file

#### Scenario: Secrets-free export
- **WHEN** the user chooses the no-secrets mode
- **THEN** the system exports metadata and key paths only and omits passwords and key contents

### Requirement: MindFS SHALL test connectivity and deploy keys for SSH servers
The system SHALL verify each saved server with a real SSH dial and SHALL offer one-click deployment of a dedicated key for password-based servers.

#### Scenario: Connection test succeeds
- **WHEN** the user tests a server whose credentials are valid and the host is reachable
- **THEN** the system dials, executes an echo command, and reports success with connection metadata such as the server banner

#### Scenario: Connection test fails with classification
- **WHEN** the dial fails due to network, authentication, or unreadable key material
- **THEN** the system returns a structured reason code for each failure class instead of raw stderr

#### Scenario: Deploy dedicated key with stored password
- **WHEN** the user triggers key deployment for a password-based server and the stored password is valid
- **THEN** the system generates an ed25519 key pair, appends the public key to the remote authorized_keys idempotently, stores the private key encrypted, and rematerializes the SSH configuration so the alias uses key authentication

#### Scenario: Key deployment fails
- **WHEN** the remote rejects the password or the authorized_keys append fails
- **THEN** the system returns a classified failure, keeps the previous authentication mode, and leaves the materialized configuration unchanged except for remaining unchanged entries

### Requirement: MindFS SHALL expose SSH server management in the sidebar UI
The system SHALL provide a sidebar entry opening an SSH server management dialog supporting listing, creating, editing, deleting, testing, importing, exporting, and credential quick reuse.

#### Scenario: Sidebar entry
- **WHEN** the user opens the left sidebar menu
- **THEN** an SSH servers entry is available alongside the existing remote servers entry and opens the management dialog

#### Scenario: Authentication mode switching
- **WHEN** the user switches between key path, inline key, and password modes in the form
- **THEN** the dialog shows only the fields relevant to the selected mode with the matching quick-reuse picker

#### Scenario: Localized text
- **WHEN** the dialog is rendered in either supported UI language
- **THEN** all labels, errors, and status messages use localized text without hardcoded strings
