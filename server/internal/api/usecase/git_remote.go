package usecase

import (
	"context"
	"errors"
	"strings"

	"mindfs/server/internal/gitview"
)

type GitRemoteInfoInput struct {
	RootID string
}

type GitRemoteInfoOutput struct {
	Info gitview.RemoteDiscoveryResult `json:"info"`
}

type GitRemoteSyncInput struct {
	RootID string
}

type GitRemoteSyncOutput struct {
	State gitview.RemoteSyncResult `json:"state"`
}

type GitRemoteFetchInput struct {
	RootID string
	Remote string
}

type GitRemoteOperationOutput struct {
	Operation gitview.RemoteOperationResult `json:"operation"`
}

type GitRemotePushUpstreamInput struct {
	RootID string
	Remote string
	Branch string
}

type GitCommitAndPushInput struct {
	RootID           string
	Message          string
	All              bool
	Paths            []string
	IncludeUntracked bool
	Remote           string
	Branch           string
}

func (s *Service) GetGitRemoteInfo(ctx context.Context, in GitRemoteInfoInput) (GitRemoteInfoOutput, error) {
	rootPath, err := s.gitRootPath(in.RootID)
	if err != nil {
		return GitRemoteInfoOutput{}, err
	}
	info, err := gitview.DiscoverRemotes(ctx, rootPath)
	if err != nil {
		return GitRemoteInfoOutput{}, err
	}
	return GitRemoteInfoOutput{Info: info}, nil
}

func (s *Service) GetGitRemoteSync(ctx context.Context, in GitRemoteSyncInput) (GitRemoteSyncOutput, error) {
	rootPath, err := s.gitRootPath(in.RootID)
	if err != nil {
		return GitRemoteSyncOutput{}, err
	}
	state, err := gitview.InspectRemoteSync(ctx, rootPath)
	if err != nil {
		return GitRemoteSyncOutput{}, err
	}
	return GitRemoteSyncOutput{State: state}, nil
}

func (s *Service) FetchGitRemote(ctx context.Context, in GitRemoteFetchInput) (GitRemoteOperationOutput, error) {
	rootPath, err := s.gitRootPath(in.RootID)
	if err != nil {
		return GitRemoteOperationOutput{}, err
	}
	operation, err := gitview.FetchRemote(ctx, rootPath, in.Remote)
	if err != nil {
		return GitRemoteOperationOutput{}, err
	}
	return GitRemoteOperationOutput{Operation: operation}, nil
}

func (s *Service) PullGitRemote(ctx context.Context, in GitRemoteSyncInput) (GitRemoteOperationOutput, error) {
	rootPath, err := s.gitRootPath(in.RootID)
	if err != nil {
		return GitRemoteOperationOutput{}, err
	}
	operation, err := gitview.PullRemote(ctx, rootPath)
	if err != nil {
		return GitRemoteOperationOutput{}, err
	}
	return GitRemoteOperationOutput{Operation: operation}, nil
}

func (s *Service) PushGitRemote(ctx context.Context, in GitRemoteSyncInput) (GitRemoteOperationOutput, error) {
	rootPath, err := s.gitRootPath(in.RootID)
	if err != nil {
		return GitRemoteOperationOutput{}, err
	}
	operation, err := gitview.PushRemote(ctx, rootPath)
	if err != nil {
		return GitRemoteOperationOutput{}, err
	}
	return GitRemoteOperationOutput{Operation: operation}, nil
}

func (s *Service) PushGitRemoteUpstream(ctx context.Context, in GitRemotePushUpstreamInput) (GitRemoteOperationOutput, error) {
	rootPath, err := s.gitRootPath(in.RootID)
	if err != nil {
		return GitRemoteOperationOutput{}, err
	}
	if strings.TrimSpace(in.Remote) == "" {
		return GitRemoteOperationOutput{}, errors.New("remote required")
	}
	if strings.TrimSpace(in.Branch) == "" {
		return GitRemoteOperationOutput{}, errors.New("branch required")
	}
	operation, err := gitview.PushRemoteFirst(ctx, rootPath, in.Remote, in.Branch)
	if err != nil {
		return GitRemoteOperationOutput{}, err
	}
	return GitRemoteOperationOutput{Operation: operation}, nil
}

func (s *Service) CommitAndPushGitRemote(ctx context.Context, in GitCommitAndPushInput) (GitRemoteOperationOutput, error) {
	rootPath, err := s.gitRootPath(in.RootID)
	if err != nil {
		return GitRemoteOperationOutput{}, err
	}
	paths := make([]string, 0, len(in.Paths))
	for _, raw := range in.Paths {
		path, err := s.normalizeGitPath(in.RootID, raw)
		if err != nil {
			return GitRemoteOperationOutput{Operation: gitview.RemoteOperationResult{Result: "blocked_invalid_paths", Message: err.Error()}}, nil
		}
		paths = append(paths, path)
	}
	operation, err := gitview.CommitAndPush(ctx, rootPath, gitview.CommitAndPushInput{
		Message:          in.Message,
		All:              in.All,
		Paths:            paths,
		IncludeUntracked: in.IncludeUntracked,
		Remote:           in.Remote,
		Branch:           in.Branch,
	})
	if err != nil {
		return GitRemoteOperationOutput{}, err
	}
	return GitRemoteOperationOutput{Operation: operation}, nil
}

func (s *Service) gitRootPath(rootID string) (string, error) {
	if err := s.ensureRegistry(); err != nil {
		return "", err
	}
	root, err := s.Registry.GetRoot(rootID)
	if err != nil {
		return "", err
	}
	return root.RootPath, nil
}

func (s *Service) normalizeGitPath(rootID, path string) (string, error) {
	if err := s.ensureRegistry(); err != nil {
		return "", err
	}
	root, err := s.Registry.GetRoot(rootID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path required")
	}
	normalized, err := root.NormalizePath(path)
	if err != nil {
		return "", err
	}
	if err := root.ValidateRelativePath(normalized); err != nil {
		return "", err
	}
	return normalized, nil
}
