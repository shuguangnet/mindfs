package remote

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	configpkg "mindfs/server/internal/config"
)

const (
	AgentPrefix = "remote:"
	ShellPrefix = "remote:"
)

var remoteIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9_-]{0,38}[a-z0-9])?$`)

type Server struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	NodeID        string `json:"node_id"`
	PairingSecret string `json:"pairing_secret,omitempty"`
	DefaultRootID string `json:"default_root_id"`
	Enabled       bool   `json:"enabled"`
}

type PublicServer struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	NodeID        string `json:"node_id"`
	DefaultRootID string `json:"default_root_id"`
	Enabled       bool   `json:"enabled"`
	HasSecret     bool   `json:"has_secret"`
}

type Store struct {
	mu       sync.RWMutex
	filePath string
}

func NewStore() (*Store, error) {
	configDir, err := configpkg.MindFSConfigDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, err
	}
	return &Store{filePath: filepath.Join(configDir, "remote-servers.json")}, nil
}

func NewStoreAt(path string) *Store {
	return &Store{filePath: path}
}

func (s *Store) List() ([]Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadLocked()
}

func (s *Store) Enabled() ([]Server, error) {
	items, err := s.List()
	if err != nil {
		return nil, err
	}
	out := make([]Server, 0, len(items))
	for _, item := range items {
		if item.Enabled {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *Store) Get(id string) (Server, bool, error) {
	id = NormalizeID(id)
	items, err := s.List()
	if err != nil {
		return Server{}, false, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, true, nil
		}
	}
	return Server{}, false, nil
}

func (s *Store) Save(server Server) (Server, error) {
	normalized, err := NormalizeServer(server)
	if err != nil {
		return Server{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return Server{}, err
	}
	replaced := false
	for i := range items {
		if items[i].ID != normalized.ID {
			continue
		}
		if strings.TrimSpace(normalized.PairingSecret) == "" {
			normalized.PairingSecret = items[i].PairingSecret
		}
		items[i] = normalized
		replaced = true
		break
	}
	if !replaced {
		if strings.TrimSpace(normalized.PairingSecret) == "" {
			return Server{}, errors.New("pairing_secret required")
		}
		items = append(items, normalized)
	}
	return normalized, s.saveLocked(items)
}

func (s *Store) Delete(id string) error {
	id = NormalizeID(id)
	if id == "" {
		return errors.New("remote server id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return err
	}
	next := items[:0]
	for _, item := range items {
		if item.ID != id {
			next = append(next, item)
		}
	}
	return s.saveLocked(next)
}

func (s *Store) loadLocked() ([]Server, error) {
	var payload struct {
		Servers []Server `json:"servers"`
	}
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Server{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return []Server{}, nil
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	out := make([]Server, 0, len(payload.Servers))
	for _, item := range payload.Servers {
		normalized, err := NormalizeServer(item)
		if err == nil {
			out = append(out, normalized)
		}
	}
	return out, nil
}

func (s *Store) saveLocked(items []Server) error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(struct {
		Servers []Server `json:"servers"`
	}{Servers: items}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, append(payload, '\n'), 0o600)
}

func NormalizeID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizeServer(server Server) (Server, error) {
	server.ID = NormalizeID(server.ID)
	if !remoteIDPattern.MatchString(server.ID) {
		return Server{}, errors.New("invalid remote server id")
	}
	server.Name = strings.TrimSpace(server.Name)
	if server.Name == "" {
		server.Name = server.ID
	}
	baseURL, err := normalizeBaseURL(server.BaseURL)
	if err != nil {
		return Server{}, err
	}
	server.BaseURL = baseURL
	server.NodeID = strings.TrimSpace(server.NodeID)
	server.PairingSecret = strings.TrimSpace(server.PairingSecret)
	server.DefaultRootID = strings.TrimSpace(server.DefaultRootID)
	if server.NodeID == "" {
		return Server{}, errors.New("node_id required")
	}
	if server.DefaultRootID == "" {
		return Server{}, errors.New("default_root_id required")
	}
	return server, nil
}

func Public(server Server) PublicServer {
	return PublicServer{
		ID:            server.ID,
		Name:          server.Name,
		BaseURL:       server.BaseURL,
		NodeID:        server.NodeID,
		DefaultRootID: server.DefaultRootID,
		Enabled:       server.Enabled,
		HasSecret:     strings.TrimSpace(server.PairingSecret) != "",
	}
}

func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("invalid remote base_url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("invalid remote base_url")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func EncodeAgentName(serverID, agentName string) string {
	return AgentPrefix + NormalizeID(serverID) + ":" + strings.TrimSpace(agentName)
}

func EncodeShellID(serverID, shellID string) string {
	return ShellPrefix + NormalizeID(serverID) + ":" + strings.TrimSpace(shellID)
}

func ParseName(value string) (serverID string, inner string, ok bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, AgentPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(value, AgentPrefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	serverID = NormalizeID(parts[0])
	inner = strings.TrimSpace(parts[1])
	return serverID, inner, serverID != "" && inner != ""
}
