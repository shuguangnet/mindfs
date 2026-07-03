## ADDED Requirements

### Requirement: MindFS SHALL discover configured remote repositories
When a managed root belongs to a Git repository, the system SHALL provide a structured list of configured Git remotes and SHALL fetch online remote metadata when the remote is reachable through the local Git credential chain.

#### Scenario: Repository has configured remotes
- **WHEN** the user requests remote repository information for a managed root with configured remotes
- **THEN** the system returns each remote name, a sanitized remote URL summary, and whether online metadata was fetched successfully

#### Scenario: Reachable remote exposes branch metadata
- **WHEN** the user requests remote repository information and the configured remote can be reached
- **THEN** the system returns the remote default branch when it can be determined and a bounded summary of remote branch heads

#### Scenario: Remote URL contains credentials
- **WHEN** the system returns remote repository information for an HTTPS remote URL that contains a username, password, or token-like credential
- **THEN** the response masks the credential material before returning the URL summary to the client

#### Scenario: Remote metadata cannot be fetched online
- **WHEN** the user requests remote repository information and Git cannot authenticate, connect, or access the remote repository
- **THEN** the system returns the configured remote name and sanitized URL summary with a structured unreachable reason

#### Scenario: Repository has no configured remotes
- **WHEN** the user requests remote repository information for a Git repository without configured remotes
- **THEN** the system returns a successful empty remote list and marks remote operations that require a remote as unavailable

### Requirement: MindFS SHALL expose remote sync state for the current branch
When a managed root belongs to a Git repository, the system SHALL provide structured remote sync state for the current branch and its configured upstream.

#### Scenario: Current branch tracks an upstream branch
- **WHEN** the user requests remote sync state for a managed root whose current branch tracks an upstream branch
- **THEN** the system returns the upstream remote name, upstream branch name, ahead count, behind count, and whether the worktree has uncommitted changes

#### Scenario: Current branch has no upstream branch
- **WHEN** the user requests remote sync state for a managed root whose current branch does not track an upstream branch
- **THEN** the system returns a successful response that marks pull and default push unavailable because no upstream is configured

#### Scenario: Repository is not on a branch
- **WHEN** the user requests remote sync state for a managed root whose repository is in detached HEAD state
- **THEN** the system returns a successful response that marks branch-based pull and push unavailable because no current branch is checked out

### Requirement: MindFS SHALL fetch remote updates without mutating the worktree
The system SHALL allow the user to fetch remote updates for a managed repository without changing checked out worktree files.

#### Scenario: Fetch succeeds
- **WHEN** the user triggers fetch for a managed root with a reachable configured remote
- **THEN** the system updates remote-tracking references and returns a successful fetch result

#### Scenario: Fetch fails because remote access is unavailable
- **WHEN** the user triggers fetch and Git cannot authenticate, connect, or access the remote repository
- **THEN** the system returns a failed result with a structured reason and preserves the existing worktree contents

### Requirement: MindFS SHALL pull remote updates using a safe one-click branch update
The system SHALL allow the user to pull remote updates with one action only when the operation can complete without silently overwriting local work or creating an implicit merge commit.

#### Scenario: Pull fast-forwards successfully
- **WHEN** the user triggers pull on a clean branch that is behind its upstream and can be fast-forwarded
- **THEN** the system updates the local branch, returns a `fast_forwarded` result, and refreshes Git status, history, and remote sync state

#### Scenario: Pull reports already up to date
- **WHEN** the user triggers pull on a branch that is already aligned with its upstream
- **THEN** the system returns an `up_to_date` result and leaves worktree contents unchanged

#### Scenario: Pull is blocked by uncommitted local changes
- **WHEN** the user triggers pull on a repository with uncommitted worktree changes
- **THEN** the system rejects the operation with a `blocked_dirty` result before applying remote changes

#### Scenario: Pull is blocked because no upstream exists
- **WHEN** the user triggers pull on a branch without a configured upstream
- **THEN** the system rejects the operation with a `blocked_no_upstream` result

#### Scenario: Pull is blocked because fast-forward is not possible
- **WHEN** the user triggers pull on a branch that has diverged from its upstream and requires a merge or rebase
- **THEN** the system rejects the operation with a `blocked_non_ff` result

### Requirement: MindFS SHALL push local commits to the configured upstream
The system SHALL allow the user to push the current branch to its configured upstream when doing so does not require rewriting remote history.

#### Scenario: Push succeeds for an ahead branch
- **WHEN** the user triggers push on a current branch with a configured upstream and local commits that can be accepted by the remote
- **THEN** the system pushes the commits, returns a `pushed` result, and refreshes Git status, history, and remote sync state

#### Scenario: Push reports no commits to publish
- **WHEN** the user triggers push on a current branch that has no local commits ahead of its upstream
- **THEN** the system returns an `up_to_date` result without treating the operation as an error

#### Scenario: Push is blocked because no upstream exists
- **WHEN** the user triggers a default push on a current branch without a configured upstream
- **THEN** the system rejects the operation with a `blocked_no_upstream` result and includes available remotes when they can be determined

#### Scenario: Push is rejected by the remote
- **WHEN** the user triggers push and the remote rejects the update because the remote branch contains work not present locally
- **THEN** the system returns a `rejected_non_ff` result and does not retry with force push

#### Scenario: Push fails because remote access is unavailable
- **WHEN** the user triggers push and Git cannot authenticate, connect, or access the remote repository
- **THEN** the system returns a failed result with a structured reason and does not mark local commits as published

### Requirement: MindFS SHALL support setting upstream during first push
The system SHALL allow the user to publish the current branch to a selected remote branch and set that branch as upstream when the current branch has no upstream.

#### Scenario: First push sets upstream successfully
- **WHEN** the user triggers push with an explicit remote name and remote branch for a current branch without upstream
- **THEN** the system pushes the branch, configures the selected remote branch as upstream, returns a `pushed` result, and refreshes remote sync state

#### Scenario: First push validates remote selection
- **WHEN** the user triggers first push with a remote name that is not configured for the repository
- **THEN** the system rejects the operation with an invalid request result before invoking Git push

### Requirement: MindFS SHALL commit selected changes and push them in one workflow
The system SHALL allow the user to create a local commit from selected repository changes and then push that commit to the configured or explicitly selected remote target.

#### Scenario: Commit and push succeeds
- **WHEN** the user triggers commit-and-push with a non-empty commit message, a valid change scope, and a push target that accepts the update
- **THEN** the system stages the requested changes, creates a local commit, pushes it to the target remote branch, returns a `committed_and_pushed` result with the commit hash, and refreshes Git status, history, and remote sync state

#### Scenario: Commit and push blocks an empty message
- **WHEN** the user triggers commit-and-push with an empty or whitespace-only commit message
- **THEN** the system rejects the operation with a `blocked_empty_message` result before staging or committing changes

#### Scenario: Commit and push blocks when there are no selected changes
- **WHEN** the user triggers commit-and-push with a valid message but the requested scope contains no changed files to commit
- **THEN** the system rejects the operation with a `blocked_no_changes` result before invoking Git commit

#### Scenario: Commit and push validates selected paths
- **WHEN** the user triggers commit-and-push with a path outside the target repository or outside the allowed managed root scope
- **THEN** the system rejects the operation with a `blocked_invalid_paths` result before staging or committing changes

#### Scenario: Commit succeeds but push is rejected
- **WHEN** the user triggers commit-and-push and the local commit is created but the remote rejects the push because the remote branch contains work not present locally
- **THEN** the system returns a `committed_push_failed` result with the new commit hash and a `rejected_non_ff` push reason, and it does not retry with force push

#### Scenario: Commit succeeds but remote access fails
- **WHEN** the user triggers commit-and-push and the local commit is created but Git cannot authenticate, connect, or access the remote repository during push
- **THEN** the system returns a `committed_push_failed` result with the new commit hash and a structured push failure reason

#### Scenario: Commit and push requires an explicit target when no upstream exists
- **WHEN** the user triggers commit-and-push on a branch without a configured upstream
- **THEN** the system requires an explicit remote name and remote branch before pushing and rejects the request with `blocked_no_upstream` if no target is provided

### Requirement: MindFS SHALL present remote repository operations with clear UI states
The client SHALL expose remote repository metadata and sync actions near the existing Git status context and SHALL keep operation state visible while requests are in progress.

#### Scenario: Operation is in progress
- **WHEN** the user starts fetch, pull, push, or commit-and-push from the Git UI
- **THEN** the client disables conflicting remote actions for the same root and shows that the selected operation is running

#### Scenario: Operation completes
- **WHEN** fetch, pull, push, or commit-and-push returns a structured result
- **THEN** the client presents a concise success, blocked, or failed message based on the result code and refreshes repository data when the operation changed repository state

#### Scenario: Commit succeeds but push fails in the UI
- **WHEN** commit-and-push returns a `committed_push_failed` result
- **THEN** the client clearly shows that the local commit was created, includes the commit hash when available, and keeps push or pull recovery actions accessible
