## ADDED Requirements

### Requirement: Export a MindFS session to a native pi session file

The system SHALL be able to convert a stored MindFS session into the native pi session-file format and either write it into pi's session directory or return it as a download.

#### Scenario: write mode
- **WHEN** the user triggers "export to native pi" on a pi session with mode `write`
- **THEN** the server writes `~/.pi/agent/sessions/<munged-cwd>/<timestamp>_<sessionId>.jsonl` (0600) where the directory name matches pi's cwd-munging rule and the file uses session format version 3 with a valid parent-id message chain of user and assistant text entries
- **AND** the response reports the absolute file path

#### Scenario: id linkage preserves resume
- **WHEN** the session has a pi agent binding with an agent session id
- **THEN** the exported file uses that same session id, so both MindFS resume and bare `pi resume` can continue the conversation afterwards

#### Scenario: idempotent re-export
- **WHEN** a native file with the same session id already exists
- **THEN** the existing file is left untouched and the export reports that path as already present

#### Scenario: download mode
- **WHEN** mode is `download`
- **THEN** the generated jsonl is returned as an attachment without writing to pi's directory

#### Scenario: non-pi session rejected
- **WHEN** the target session's agent is not pi (and no pi exporter exists)
- **THEN** the API responds with a structured unsupported-agent error
