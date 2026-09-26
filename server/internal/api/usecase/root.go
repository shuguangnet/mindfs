package usecase

import (
	"errors"

	"mindfs/server/internal/agent"
	agenttypes "mindfs/server/internal/agent/types"
	"mindfs/server/internal/fs"
	"mindfs/server/internal/preferences"
	"mindfs/server/internal/session"
	"mindfs/server/internal/usage"
)

type Registry interface {
	GetRoot(rootID string) (fs.RootInfo, error)
	GetSessionManager(rootID string) (*session.Manager, error)
	UpsertRoot(path string) (fs.RootInfo, error)
	RemoveRoot(path string) (fs.RootInfo, error)
	RenameRoot(rootID, name, rootPath string) (fs.RootInfo, error)
	ListRoots() []fs.RootInfo
	GetAgentPool() *agent.Pool
	GetPreferences() *preferences.Store
	GetExternalSessionImporter(agentName string) (agenttypes.ExternalSessionImporter, error)
	GetProber() *agent.Prober
	GetCandidateRegistry() *CandidateRegistry
	GetFileWatcher(rootID string, manager *session.Manager) (*fs.SharedFileWatcher, error)
	ReleaseFileWatcher(rootID, sessionKey string)
	// GetUsageStore exposes the token price table and daily budget.
	GetUsageStore() *usage.Store
}

type Service struct {
	Registry Registry
}

func (s *Service) ensureRegistry() error {
	if s == nil || s.Registry == nil {
		return errors.New("services not configured")
	}
	return nil
}
