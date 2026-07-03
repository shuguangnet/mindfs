package gitview

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeRemoteURLMasksCredentials(t *testing.T) {
	tests := map[string]string{
		"https://token@example.com/owner/repo.git":       "https://%2A%2A%2A@example.com/owner/repo.git",
		"https://user:secret@example.com/owner/repo.git": "https://%2A%2A%2A@example.com/owner/repo.git",
		"ssh://user@example.com/owner/repo.git":          "ssh://%2A%2A%2A@example.com/owner/repo.git",
		"git@github.com:owner/repo.git":                  "git@github.com:owner/repo.git",
		"https://github.com/owner/repo.git":              "https://github.com/owner/repo.git",
	}
	for raw, want := range tests {
		if got := sanitizeRemoteURL(raw); got != want {
			t.Fatalf("sanitizeRemoteURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestRemoteDiscoveryAndSyncState(t *testing.T) {
	ctx := context.Background()
	work, _ := setupRemoteRepo(t)

	remotes, err := DiscoverRemotes(ctx, work)
	if err != nil {
		t.Fatalf("DiscoverRemotes: %v", err)
	}
	if !remotes.Available || remotes.Status != "listed" || len(remotes.Remotes) != 1 {
		t.Fatalf("remote discovery = %+v", remotes)
	}
	if remotes.Remotes[0].Name != "origin" || remotes.Remotes[0].Status != "listed" {
		t.Fatalf("origin remote = %+v", remotes.Remotes[0])
	}

	state, err := InspectRemoteSync(ctx, work)
	if err != nil {
		t.Fatalf("InspectRemoteSync: %v", err)
	}
	if !state.Available || state.State != "ready" || state.Upstream != "origin/main" || state.Ahead != 0 || state.Behind != 0 || state.Dirty {
		t.Fatalf("sync state = %+v", state)
	}
}

func TestFetchPullPushAndFirstPush(t *testing.T) {
	ctx := context.Background()
	work, bare := setupRemoteRepo(t)
	other := cloneRepo(t, bare)
	writeFile(t, filepath.Join(other, "remote.txt"), "remote\n")
	runTestGit(t, other, "add", "remote.txt")
	runTestGit(t, other, "commit", "-m", "remote update")
	runTestGit(t, other, "push")

	fetch, err := FetchRemote(ctx, work, "origin")
	if err != nil {
		t.Fatalf("FetchRemote: %v", err)
	}
	if fetch.Result != "fetched" {
		t.Fatalf("fetch result = %+v", fetch)
	}

	pull, err := PullRemote(ctx, work)
	if err != nil {
		t.Fatalf("PullRemote: %v", err)
	}
	if pull.Result != "fast_forwarded" {
		t.Fatalf("pull result = %+v", pull)
	}

	writeFile(t, filepath.Join(work, "local.txt"), "local\n")
	runTestGit(t, work, "add", "local.txt")
	runTestGit(t, work, "commit", "-m", "local update")
	push, err := PushRemote(ctx, work)
	if err != nil {
		t.Fatalf("PushRemote: %v", err)
	}
	if push.Result != "pushed" {
		t.Fatalf("push result = %+v", push)
	}

	runTestGit(t, work, "checkout", "-b", "feature/first-push")
	writeFile(t, filepath.Join(work, "feature.txt"), "feature\n")
	runTestGit(t, work, "add", "feature.txt")
	runTestGit(t, work, "commit", "-m", "feature")
	first, err := PushRemoteFirst(ctx, work, "origin", "feature/first-push")
	if err != nil {
		t.Fatalf("PushRemoteFirst: %v", err)
	}
	if first.Result != "pushed" {
		t.Fatalf("first push result = %+v", first)
	}
}

func TestCommitAndPushValidationAndSuccess(t *testing.T) {
	ctx := context.Background()
	work, bare := setupRemoteRepo(t)

	empty, err := CommitAndPush(ctx, work, CommitAndPushInput{Message: "  ", All: true, IncludeUntracked: true})
	if err != nil {
		t.Fatalf("CommitAndPush empty message: %v", err)
	}
	if empty.Result != "blocked_empty_message" {
		t.Fatalf("empty message result = %+v", empty)
	}

	noChanges, err := CommitAndPush(ctx, work, CommitAndPushInput{Message: "nothing", All: true, IncludeUntracked: true})
	if err != nil {
		t.Fatalf("CommitAndPush no changes: %v", err)
	}
	if noChanges.Result != "blocked_no_changes" {
		t.Fatalf("no changes result = %+v", noChanges)
	}

	writeFile(t, filepath.Join(work, "commit-push.txt"), "commit and push\n")
	success, err := CommitAndPush(ctx, work, CommitAndPushInput{Message: "commit and push", All: true, IncludeUntracked: true})
	if err != nil {
		t.Fatalf("CommitAndPush success: %v", err)
	}
	if success.Result != "committed_and_pushed" || success.CommitHash == "" {
		t.Fatalf("success result = %+v", success)
	}
	remoteHead := strings.TrimSpace(runTestGitDir(t, "", "--git-dir", bare, "rev-parse", "refs/heads/main"))
	if remoteHead != success.CommitHash {
		t.Fatalf("remote head = %q, want commit %q", remoteHead, success.CommitHash)
	}

	invalid, err := CommitAndPush(ctx, work, CommitAndPushInput{Message: "invalid", Paths: []string{"../outside"}})
	if err != nil {
		t.Fatalf("CommitAndPush invalid path: %v", err)
	}
	if invalid.Result != "blocked_invalid_paths" {
		t.Fatalf("invalid path result = %+v", invalid)
	}
}

func TestCommitAndPushBlocksNoUpstreamBeforeCommit(t *testing.T) {
	ctx := context.Background()
	work, _ := setupRemoteRepo(t)
	runTestGit(t, work, "checkout", "-b", "local-only")
	writeFile(t, filepath.Join(work, "local-only.txt"), "local\n")

	result, err := CommitAndPush(ctx, work, CommitAndPushInput{Message: "local only", All: true, IncludeUntracked: true})
	if err != nil {
		t.Fatalf("CommitAndPush no upstream: %v", err)
	}
	if result.Result != "blocked_no_upstream" {
		t.Fatalf("no upstream result = %+v", result)
	}
	if got := strings.TrimSpace(runTestGit(t, work, "status", "--porcelain")); got == "" {
		t.Fatalf("expected changes to remain uncommitted")
	}
}

func TestPullBlocksDirtyAndNonFastForward(t *testing.T) {
	ctx := context.Background()
	work, bare := setupRemoteRepo(t)
	other := cloneRepo(t, bare)
	writeFile(t, filepath.Join(other, "remote.txt"), "remote\n")
	runTestGit(t, other, "add", "remote.txt")
	runTestGit(t, other, "commit", "-m", "remote update")
	runTestGit(t, other, "push")

	writeFile(t, filepath.Join(work, "dirty.txt"), "dirty\n")
	dirty, err := PullRemote(ctx, work)
	if err != nil {
		t.Fatalf("PullRemote dirty: %v", err)
	}
	if dirty.Result != "blocked_dirty" {
		t.Fatalf("dirty pull result = %+v", dirty)
	}
	runTestGit(t, work, "reset", "--hard")
	runTestGit(t, work, "clean", "-fd")

	writeFile(t, filepath.Join(work, "local-diverge.txt"), "local\n")
	runTestGit(t, work, "add", "local-diverge.txt")
	runTestGit(t, work, "commit", "-m", "local diverge")
	diverged, err := PullRemote(ctx, work)
	if err != nil {
		t.Fatalf("PullRemote diverged: %v", err)
	}
	if diverged.Result != "blocked_non_ff" {
		t.Fatalf("diverged pull result = %+v", diverged)
	}
}

func TestPushRejectedAndCommitPushPartialFailure(t *testing.T) {
	ctx := context.Background()
	work, bare := setupRemoteRepo(t)
	other := cloneRepo(t, bare)
	writeFile(t, filepath.Join(other, "remote-only.txt"), "remote\n")
	runTestGit(t, other, "add", "remote-only.txt")
	runTestGit(t, other, "commit", "-m", "remote only")
	runTestGit(t, other, "push")

	writeFile(t, filepath.Join(work, "local-only.txt"), "local\n")
	runTestGit(t, work, "add", "local-only.txt")
	runTestGit(t, work, "commit", "-m", "local only")
	rejected, err := PushRemote(ctx, work)
	if err != nil {
		t.Fatalf("PushRemote rejected: %v", err)
	}
	if rejected.Result != "rejected_non_ff" {
		t.Fatalf("push rejected result = %+v", rejected)
	}

	writeFile(t, filepath.Join(work, "partial.txt"), "partial\n")
	partial, err := CommitAndPush(ctx, work, CommitAndPushInput{Message: "partial", All: true, IncludeUntracked: true})
	if err != nil {
		t.Fatalf("CommitAndPush partial: %v", err)
	}
	if partial.Result != "committed_push_failed" || partial.PushResult != "rejected_non_ff" || partial.CommitHash == "" {
		t.Fatalf("partial result = %+v", partial)
	}
}

func TestRemoteFailureClassification(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		{"fatal: Authentication failed for 'https://example.invalid/repo.git'", "failed_auth"},
		{"ssh: connect to host example.invalid port 22: Network is unreachable", "failed_network"},
		{"fatal: 'missing.git' does not appear to be a git repository", "failed_remote"},
		{"fatal: unknown failure", "failed"},
	}
	for _, tt := range tests {
		if got := classifyRemoteFailure(fakeGitError(tt.message)); got != tt.want {
			t.Fatalf("classifyRemoteFailure(%q) = %q, want %q", tt.message, got, tt.want)
		}
	}
	if got := classifyRemoteReachability(fakeGitError("Authentication failed")); got != "unreachable_auth" {
		t.Fatalf("classifyRemoteReachability auth = %q", got)
	}
}

type fakeGitError string

func (e fakeGitError) Error() string { return string(e) }

func setupRemoteRepo(t *testing.T) (work string, bare string) {
	t.Helper()
	root := t.TempDir()
	bare = filepath.Join(root, "remote.git")
	work = filepath.Join(root, "work")
	runTestGitDir(t, "", "init", "--bare", bare)
	runTestGitDir(t, "", "init", work)
	runTestGit(t, work, "config", "user.name", "MindFS Test")
	runTestGit(t, work, "config", "user.email", "mindfs@example.invalid")
	writeFile(t, filepath.Join(work, "README.md"), "initial\n")
	runTestGit(t, work, "add", "README.md")
	runTestGit(t, work, "commit", "-m", "initial")
	runTestGit(t, work, "branch", "-M", "main")
	runTestGit(t, work, "remote", "add", "origin", bare)
	runTestGit(t, work, "push", "-u", "origin", "main")
	runTestGitDir(t, "", "--git-dir", bare, "symbolic-ref", "HEAD", "refs/heads/main")
	return work, bare
}

func cloneRepo(t *testing.T, bare string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clone")
	runTestGitDir(t, "", "clone", bare, path)
	runTestGit(t, path, "config", "user.name", "MindFS Test")
	runTestGit(t, path, "config", "user.email", "mindfs@example.invalid")
	return path
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return runTestGitDir(t, dir, args...)
}

func runTestGitDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}
