## 1. Backend Git Remote Primitives

- [x] 1.1 Add remote sync state and operation result types in `server/internal/gitview`, including upstream identity, ahead/behind counts, dirty state, available remotes, and stable result codes.
- [x] 1.2 Implement current branch and upstream inspection, including no-upstream and detached-HEAD classification.
- [x] 1.3 Implement configured remote discovery, sanitized remote URL summaries, and bounded online metadata lookup for default branch and remote branch heads.
- [x] 1.4 Implement remote reachability classification for auth, network, missing remote, and generic failures without mutating the worktree.
- [x] 1.5 Implement fetch execution that updates remote-tracking refs without mutating the worktree and maps auth/network/remote failures to structured results.
- [x] 1.6 Implement safe pull execution that rejects dirty worktrees, missing upstream, detached HEAD, and non-fast-forward updates before returning structured results.
- [x] 1.7 Implement push execution for the current branch upstream, including up-to-date detection, non-fast-forward rejection mapping, and no force-push behavior.
- [x] 1.8 Implement first-push support with explicit remote and branch inputs, remote validation, upstream setup, and refreshed sync state.
- [x] 1.9 Implement commit scope validation and local commit creation primitives for explicit paths or all changes, including empty message, empty change set, and invalid path classification.
- [x] 1.10 Implement commit-and-push orchestration that stages, commits, pushes, and returns `committed_push_failed` with commit hash when push fails after commit.

## 2. API and Usecase Integration

- [x] 2.1 Add usecase request/response models for remote repository discovery, remote sync state, fetch, pull, default push, first push, and commit-and-push.
- [x] 2.2 Expose HTTP endpoints for remote repository state and operations with root validation and request payload validation.
- [x] 2.3 Add commit-and-push request validation for message, paths, all-changes mode, include-untracked behavior, and explicit push targets when no upstream exists.
- [x] 2.4 Ensure successful pull, push, and commit-and-push operations refresh or invalidate Git status, history, diff, and remote sync data consistently.
- [x] 2.5 Return structured API errors and operation result payloads without requiring clients to parse raw Git stdout or stderr.
- [x] 2.6 Ensure API responses never expose credential material embedded in remote URLs or Git command diagnostics.

## 3. Web Git Remote Workflow

- [x] 3.1 Extend `web/src/services/git.ts` with remote repository discovery, remote sync state, fetch, pull, push, first-push, and commit-and-push client helpers.
- [x] 3.2 Add sanitized remote summary, upstream, ahead/behind, and dirty-state presentation to the existing Git status context.
- [x] 3.3 Add fetch, one-click pull, and push controls near the Git status panel header or toolbar with per-root in-progress state and disabled conflicting actions.
- [x] 3.4 Add first-push UI for branches without upstream, including remote selection, branch name confirmation, and clear target display.
- [x] 3.5 Add commit-and-push UI with commit message input, change scope selection, target display, validation feedback, and operation progress.
- [x] 3.6 Map structured operation results to concise success, blocked, rejected, partial-success, and failed messages, then refresh repository views after state-changing operations.
- [x] 3.7 Keep recovery actions visible when commit-and-push returns `committed_push_failed`, including follow-up push or pull entry points.

## 4. Verification

- [x] 4.1 Add backend tests using temporary repositories and a local bare remote for remote discovery, upstream state, fetch, fast-forward pull, default push, first push, and commit-and-push.
- [x] 4.2 Add backend tests for no upstream, detached HEAD, dirty worktree pull block, non-fast-forward pull block, push rejection, missing/invalid remote cases, empty commit message, no changes, invalid paths, and committed-but-push-failed cases.
- [x] 4.3 Add backend tests or fixtures for remote URL credential masking and online metadata failure classification.
- [ ] 4.4 Add frontend tests or focused component/service coverage for remote action disabled states, first-push form validation, commit-and-push validation, partial-success messaging, and result message mapping.
- [x] 4.5 Run the relevant Go and web test suites and validate the OpenSpec change before marking implementation complete.
