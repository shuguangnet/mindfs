package gitview

import (
	"bufio"
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const maxRemoteBranches = 20

type RemoteBranchInfo struct {
	Name string `json:"name"`
	Hash string `json:"hash,omitempty"`
}

type RemoteInfo struct {
	Name          string             `json:"name"`
	FetchURL      string             `json:"fetch_url,omitempty"`
	PushURL       string             `json:"push_url,omitempty"`
	Status        string             `json:"status"`
	Reason        string             `json:"reason,omitempty"`
	DefaultBranch string             `json:"default_branch,omitempty"`
	Branches      []RemoteBranchInfo `json:"branches,omitempty"`
}

type RemoteDiscoveryResult struct {
	Available bool         `json:"available"`
	Status    string       `json:"status"`
	Remotes   []RemoteInfo `json:"remotes"`
}

type RemoteSyncResult struct {
	Available      bool         `json:"available"`
	State          string       `json:"state"`
	CurrentBranch  string       `json:"current_branch,omitempty"`
	Upstream       string       `json:"upstream,omitempty"`
	UpstreamRemote string       `json:"upstream_remote,omitempty"`
	UpstreamBranch string       `json:"upstream_branch,omitempty"`
	Ahead          int          `json:"ahead"`
	Behind         int          `json:"behind"`
	Dirty          bool         `json:"dirty"`
	DirtyCount     int          `json:"dirty_count"`
	Remotes        []RemoteInfo `json:"remotes"`
}

type RemoteOperationResult struct {
	Result     string            `json:"result"`
	Message    string            `json:"message,omitempty"`
	CommitHash string            `json:"commit_hash,omitempty"`
	PushResult string            `json:"push_result,omitempty"`
	State      *RemoteSyncResult `json:"state,omitempty"`
}

type CommitAndPushInput struct {
	Message          string   `json:"message"`
	All              bool     `json:"all"`
	Paths            []string `json:"paths,omitempty"`
	IncludeUntracked bool     `json:"include_untracked,omitempty"`
	Remote           string   `json:"remote,omitempty"`
	Branch           string   `json:"branch,omitempty"`
}

func DiscoverRemotes(ctx context.Context, rootPath string) (RemoteDiscoveryResult, error) {
	repo, err := loadRepoContext(ctx, rootPath)
	if err != nil {
		if isNotRepoError(err) {
			return RemoteDiscoveryResult{Available: false, Status: "not_git_repo", Remotes: []RemoteInfo{}}, nil
		}
		return RemoteDiscoveryResult{}, err
	}
	remotes, err := repo.discoverRemotes(ctx, true)
	if err != nil {
		return RemoteDiscoveryResult{}, err
	}
	status := "listed"
	if len(remotes) == 0 {
		status = "no_remotes"
	}
	return RemoteDiscoveryResult{Available: true, Status: status, Remotes: remotes}, nil
}

func InspectRemoteSync(ctx context.Context, rootPath string) (RemoteSyncResult, error) {
	repo, err := loadRepoContext(ctx, rootPath)
	if err != nil {
		if isNotRepoError(err) {
			return RemoteSyncResult{Available: false, State: "not_git_repo", Remotes: []RemoteInfo{}}, nil
		}
		return RemoteSyncResult{}, err
	}
	remotes, err := repo.discoverRemotes(ctx, false)
	if err != nil {
		return RemoteSyncResult{}, err
	}
	items, err := repo.statusItems(ctx)
	if err != nil {
		return RemoteSyncResult{}, err
	}
	result := RemoteSyncResult{
		Available:     true,
		State:         "ready",
		CurrentBranch: repo.currentBranch(),
		Dirty:         len(items) > 0,
		DirtyCount:    len(items),
		Remotes:       remotes,
	}
	if repo.isDetached() {
		result.State = "detached_head"
		return result, nil
	}
	upstream, err := repo.upstream(ctx)
	if err != nil || upstream == "" {
		result.State = "no_upstream"
		return result, nil
	}
	result.Upstream = upstream
	result.UpstreamRemote, result.UpstreamBranch = splitUpstream(upstream)
	ahead, behind, err := repo.aheadBehind(ctx)
	if err != nil {
		return RemoteSyncResult{}, err
	}
	result.Ahead = ahead
	result.Behind = behind
	return result, nil
}

func FetchRemote(ctx context.Context, rootPath, remote string) (RemoteOperationResult, error) {
	repo, err := loadRepoContext(ctx, rootPath)
	if err != nil {
		return RemoteOperationResult{}, err
	}
	remote = strings.TrimSpace(remote)
	args := []string{"fetch", "--prune"}
	if remote == "" {
		args = append(args, "--all")
	} else {
		if !repo.hasRemote(ctx, remote) {
			return repo.operationResult(ctx, "failed_remote", "remote not configured", ""), nil
		}
		args = append(args, remote)
	}
	if _, err := runGit(ctx, repo.repoRoot, args...); err != nil {
		return repo.operationResult(ctx, classifyRemoteFailure(err), "", ""), nil
	}
	return repo.operationResult(ctx, "fetched", "", ""), nil
}

func PullRemote(ctx context.Context, rootPath string) (RemoteOperationResult, error) {
	repo, err := loadRepoContext(ctx, rootPath)
	if err != nil {
		return RemoteOperationResult{}, err
	}
	state, err := InspectRemoteSync(ctx, rootPath)
	if err != nil {
		return RemoteOperationResult{}, err
	}
	if state.State == "detached_head" {
		return RemoteOperationResult{Result: "blocked_detached_head", State: &state}, nil
	}
	if state.State == "no_upstream" {
		return RemoteOperationResult{Result: "blocked_no_upstream", State: &state}, nil
	}
	if state.Dirty {
		return RemoteOperationResult{Result: "blocked_dirty", State: &state}, nil
	}
	before := repo.head(ctx)
	if _, err := runGit(ctx, repo.repoRoot, "pull", "--ff-only"); err != nil {
		return repo.operationResult(ctx, classifyPullFailure(err), "", ""), nil
	}
	after := repo.head(ctx)
	if before != "" && before == after {
		return repo.operationResult(ctx, "up_to_date", "", ""), nil
	}
	return repo.operationResult(ctx, "fast_forwarded", "", ""), nil
}

func PushRemote(ctx context.Context, rootPath string) (RemoteOperationResult, error) {
	repo, err := loadRepoContext(ctx, rootPath)
	if err != nil {
		return RemoteOperationResult{}, err
	}
	state, err := InspectRemoteSync(ctx, rootPath)
	if err != nil {
		return RemoteOperationResult{}, err
	}
	if state.State == "detached_head" {
		return RemoteOperationResult{Result: "blocked_detached_head", State: &state}, nil
	}
	if state.State == "no_upstream" {
		return RemoteOperationResult{Result: "blocked_no_upstream", State: &state}, nil
	}
	if state.Ahead <= 0 {
		return RemoteOperationResult{Result: "up_to_date", State: &state}, nil
	}
	if _, err := runGit(ctx, repo.repoRoot, "push"); err != nil {
		return repo.operationResult(ctx, classifyPushFailure(err), "", ""), nil
	}
	return repo.operationResult(ctx, "pushed", "", ""), nil
}

func PushRemoteFirst(ctx context.Context, rootPath, remote, branch string) (RemoteOperationResult, error) {
	repo, err := loadRepoContext(ctx, rootPath)
	if err != nil {
		return RemoteOperationResult{}, err
	}
	remote = strings.TrimSpace(remote)
	branch = strings.TrimSpace(branch)
	if repo.isDetached() {
		return repo.operationResult(ctx, "blocked_detached_head", "", ""), nil
	}
	if remote == "" || branch == "" || invalidRefInput(remote) || invalidRefInput(branch) {
		return repo.operationResult(ctx, "failed_remote", "invalid remote or branch", ""), nil
	}
	if !repo.hasRemote(ctx, remote) {
		return repo.operationResult(ctx, "failed_remote", "remote not configured", ""), nil
	}
	if _, err := runGit(ctx, repo.repoRoot, "push", "-u", remote, "HEAD:refs/heads/"+branch); err != nil {
		return repo.operationResult(ctx, classifyPushFailure(err), "", ""), nil
	}
	return repo.operationResult(ctx, "pushed", "", ""), nil
}

func CommitAndPush(ctx context.Context, rootPath string, input CommitAndPushInput) (RemoteOperationResult, error) {
	repo, err := loadRepoContext(ctx, rootPath)
	if err != nil {
		return RemoteOperationResult{}, err
	}
	message := strings.TrimSpace(input.Message)
	if message == "" {
		return repo.operationResult(ctx, "blocked_empty_message", "", ""), nil
	}
	if repo.isDetached() {
		return repo.operationResult(ctx, "blocked_detached_head", "", ""), nil
	}
	paths, err := repo.commitScopePaths(ctx, input)
	if err != nil {
		return repo.operationResult(ctx, "blocked_invalid_paths", err.Error(), ""), nil
	}
	if len(paths) == 0 {
		return repo.operationResult(ctx, "blocked_no_changes", "", ""), nil
	}
	remote := strings.TrimSpace(input.Remote)
	branch := strings.TrimSpace(input.Branch)
	if remote == "" && branch == "" {
		state, err := InspectRemoteSync(ctx, rootPath)
		if err != nil {
			return RemoteOperationResult{}, err
		}
		if state.State == "no_upstream" {
			return RemoteOperationResult{Result: "blocked_no_upstream", State: &state}, nil
		}
	}
	addArgs := append([]string{"add"}, paths...)
	if _, err := runGit(ctx, repo.repoRoot, addArgs...); err != nil {
		return repo.operationResult(ctx, "failed", "", ""), nil
	}
	commitArgs := []string{"commit", "-m", message}
	commitArgs = append(commitArgs, paths...)
	if _, err := runGit(ctx, repo.repoRoot, commitArgs...); err != nil {
		if isNoChangesError(err) {
			return repo.operationResult(ctx, "blocked_no_changes", "", ""), nil
		}
		return repo.operationResult(ctx, "failed", "", ""), nil
	}
	commitHash := repo.head(ctx)
	var push RemoteOperationResult
	if remote != "" || branch != "" {
		push, err = PushRemoteFirst(ctx, rootPath, remote, branch)
	} else {
		push, err = PushRemote(ctx, rootPath)
	}
	if err != nil {
		return RemoteOperationResult{}, err
	}
	if push.Result == "pushed" || push.Result == "up_to_date" {
		push.Result = "committed_and_pushed"
		push.CommitHash = commitHash
		push.PushResult = "pushed"
		return push, nil
	}
	state, _ := InspectRemoteSync(ctx, rootPath)
	return RemoteOperationResult{
		Result:     "committed_push_failed",
		CommitHash: commitHash,
		PushResult: push.Result,
		Message:    push.Message,
		State:      &state,
	}, nil
}

func (r repoContext) discoverRemotes(ctx context.Context, includeOnline bool) ([]RemoteInfo, error) {
	output, err := runGit(ctx, r.repoRoot, "remote", "-v")
	if err != nil {
		return nil, err
	}
	byName := map[string]*RemoteInfo{}
	order := make([]string, 0)
	scanner := bufioNewScanner(output)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		name, rawURL, kind := fields[0], fields[1], strings.Trim(fields[2], "()")
		if _, ok := byName[name]; !ok {
			byName[name] = &RemoteInfo{Name: name, Status: "listed"}
			order = append(order, name)
		}
		if kind == "push" {
			byName[name].PushURL = sanitizeRemoteURL(rawURL)
		} else {
			byName[name].FetchURL = sanitizeRemoteURL(rawURL)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	remotes := make([]RemoteInfo, 0, len(order))
	for _, name := range order {
		info := *byName[name]
		if includeOnline {
			r.enrichRemote(ctx, &info)
		}
		remotes = append(remotes, info)
	}
	return remotes, nil
}

func (r repoContext) enrichRemote(ctx context.Context, info *RemoteInfo) {
	output, err := runGit(ctx, r.repoRoot, "ls-remote", "--symref", "--heads", info.Name)
	if err != nil {
		info.Status = classifyRemoteReachability(err)
		info.Reason = info.Status
		return
	}
	info.Status = "listed"
	branches := make([]RemoteBranchInfo, 0, maxRemoteBranches)
	scanner := bufioNewScanner(output)
	seen := map[string]struct{}{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "ref:") && strings.Contains(line, "HEAD") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				info.DefaultBranch = strings.TrimPrefix(fields[1], "refs/heads/")
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[1], "refs/heads/") {
			continue
		}
		name := strings.TrimPrefix(fields[1], "refs/heads/")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		branches = append(branches, RemoteBranchInfo{Name: name, Hash: fields[0]})
		if len(branches) >= maxRemoteBranches {
			break
		}
	}
	sort.Slice(branches, func(i, j int) bool { return branches[i].Name < branches[j].Name })
	info.Branches = branches
}

func (r repoContext) upstream(ctx context.Context) (string, error) {
	output, err := runGit(ctx, r.repoRoot, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (r repoContext) aheadBehind(ctx context.Context) (int, int, error) {
	output, err := runGit(ctx, r.repoRoot, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(output)
	if len(fields) < 2 {
		return 0, 0, nil
	}
	ahead, _ := strconv.Atoi(fields[0])
	behind, _ := strconv.Atoi(fields[1])
	return ahead, behind, nil
}

func (r repoContext) head(ctx context.Context) string {
	output, err := runGit(ctx, r.repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}

func (r repoContext) currentBranch() string {
	if r.isDetached() {
		return ""
	}
	return strings.TrimSpace(r.branch)
}

func (r repoContext) isDetached() bool {
	branch := strings.TrimSpace(r.branch)
	return branch == "" || branch == "HEAD"
}

func (r repoContext) hasRemote(ctx context.Context, remote string) bool {
	remote = strings.TrimSpace(remote)
	if remote == "" || invalidRefInput(remote) {
		return false
	}
	_, err := runGit(ctx, r.repoRoot, "remote", "get-url", remote)
	return err == nil
}

func (r repoContext) commitScopePaths(ctx context.Context, input CommitAndPushInput) ([]string, error) {
	items, err := r.statusItems(ctx)
	if err != nil {
		return nil, err
	}
	changed := map[string]StatusItem{}
	for _, item := range items {
		changed[item.Path] = item
	}
	if input.All {
		if len(changed) == 0 {
			return nil, nil
		}
		if input.IncludeUntracked {
			if r.prefix != "" {
				return []string{"--", r.prefix}, nil
			}
			return []string{"--", "."}, nil
		}
		paths := []string{"--"}
		seen := map[string]struct{}{}
		for _, item := range changed {
			if item.Status == "??" {
				continue
			}
			repoPath := r.toRepoPath(item.Path)
			if _, ok := seen[repoPath]; ok {
				continue
			}
			seen[repoPath] = struct{}{}
			paths = append(paths, repoPath)
		}
		if len(paths) == 1 {
			return nil, nil
		}
		return paths, nil
	}
	paths := make([]string, 0, len(input.Paths)+1)
	paths = append(paths, "--")
	seen := map[string]struct{}{}
	for _, raw := range input.Paths {
		rel, err := normalizeCommitPath(raw)
		if err != nil {
			return nil, err
		}
		matched := false
		for _, item := range changed {
			if item.Path == rel || strings.HasPrefix(item.Path, strings.TrimSuffix(rel, "/")+"/") {
				if item.Status == "??" && !input.IncludeUntracked {
					continue
				}
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		repoPath := r.toRepoPath(rel)
		if _, ok := seen[repoPath]; ok {
			continue
		}
		seen[repoPath] = struct{}{}
		paths = append(paths, repoPath)
	}
	if len(paths) == 1 {
		return nil, nil
	}
	return paths, nil
}

func (r repoContext) operationResult(ctx context.Context, result, message, commitHash string) RemoteOperationResult {
	state, _ := InspectRemoteSync(ctx, r.repoRoot)
	return RemoteOperationResult{Result: result, Message: message, CommitHash: commitHash, State: &state}
}

func splitUpstream(upstream string) (string, string) {
	remote, branch, ok := strings.Cut(strings.TrimSpace(upstream), "/")
	if !ok {
		return "", upstream
	}
	return remote, branch
}

func sanitizeRemoteURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.Scheme != "" && parsed.User != nil {
		parsed.User = url.User("***")
		return parsed.String()
	}
	if at := strings.Index(value, "@"); at > 0 {
		prefix := value[:at]
		if strings.Contains(prefix, "://") {
			return value[:strings.LastIndex(prefix, "://")+3] + "***" + value[at:]
		}
	}
	return value
}

func normalizeCommitPath(raw string) (string, error) {
	path := filepath.ToSlash(filepath.Clean(strings.TrimSpace(raw)))
	if path == "" || path == "." {
		return "", errors.New("invalid path")
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "../") || path == ".." || strings.Contains(path, "\x00") {
		return "", errors.New("invalid path")
	}
	return path, nil
}

func invalidRefInput(value string) bool {
	return strings.ContainsAny(value, "\x00\r\n\t ") || strings.Contains(value, "..") || strings.HasPrefix(value, "-")
}

func classifyRemoteReachability(err error) string {
	result := classifyRemoteFailure(err)
	switch result {
	case "failed_auth":
		return "unreachable_auth"
	case "failed_network":
		return "unreachable_network"
	case "failed_remote":
		return "unreachable_remote"
	default:
		return "failed"
	}
}

func classifyPullFailure(err error) string {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "not possible to fast-forward") || strings.Contains(msg, "non-fast-forward") || strings.Contains(msg, "divergent") {
		return "blocked_non_ff"
	}
	return classifyRemoteFailure(err)
}

func classifyPushFailure(err error) string {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "non-fast-forward") || strings.Contains(msg, "fetch first") || strings.Contains(msg, "rejected") {
		return "rejected_non_ff"
	}
	return classifyRemoteFailure(err)
}

func classifyRemoteFailure(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "authentication failed") || strings.Contains(msg, "permission denied") || strings.Contains(msg, "could not read username") || strings.Contains(msg, "publickey"):
		return "failed_auth"
	case strings.Contains(msg, "could not resolve host") || strings.Contains(msg, "failed to connect") || strings.Contains(msg, "network is unreachable") || strings.Contains(msg, "connection timed out"):
		return "failed_network"
	case strings.Contains(msg, "repository not found") || strings.Contains(msg, "does not appear to be a git repository") || strings.Contains(msg, "couldn't find remote ref"):
		return "failed_remote"
	default:
		return "failed"
	}
}

func isNoChangesError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "nothing to commit") || strings.Contains(msg, "no changes added")
}

func bufioNewScanner(text string) *bufio.Scanner {
	return bufio.NewScanner(strings.NewReader(text))
}
