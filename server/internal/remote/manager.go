package remote

import (
	"context"
	"errors"
	"sync"
)

type Manager struct {
	store *Store

	mu      sync.Mutex
	clients map[string]*Client
}

func NewManager(store *Store) *Manager {
	return &Manager{
		store:   store,
		clients: make(map[string]*Client),
	}
}

func (m *Manager) Store() *Store {
	if m == nil {
		return nil
	}
	return m.store
}

func (m *Manager) List() ([]Server, error) {
	if m == nil || m.store == nil {
		return []Server{}, nil
	}
	return m.store.List()
}

func (m *Manager) PublicList() ([]PublicServer, error) {
	items, err := m.List()
	if err != nil {
		return nil, err
	}
	out := make([]PublicServer, 0, len(items))
	for _, item := range items {
		out = append(out, Public(item))
	}
	return out, nil
}

func (m *Manager) Save(server Server) (Server, error) {
	if m == nil || m.store == nil {
		return Server{}, errors.New("remote store not configured")
	}
	saved, err := m.store.Save(server)
	if err != nil {
		return Server{}, err
	}
	m.mu.Lock()
	delete(m.clients, saved.ID)
	m.mu.Unlock()
	return saved, nil
}

func (m *Manager) Delete(id string) error {
	if m == nil || m.store == nil {
		return errors.New("remote store not configured")
	}
	if err := m.store.Delete(id); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.clients, NormalizeID(id))
	m.mu.Unlock()
	return nil
}

func (m *Manager) Client(id string) (*Client, Server, bool, error) {
	if m == nil || m.store == nil {
		return nil, Server{}, false, nil
	}
	id = NormalizeID(id)
	server, ok, err := m.store.Get(id)
	if err != nil || !ok {
		return nil, Server{}, ok, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if client := m.clients[id]; client != nil {
		return client, server, true, nil
	}
	client := NewClient(server)
	m.clients[id] = client
	return client, server, true, nil
}

func (m *Manager) Test(ctx context.Context, id string) ([]RootInfo, AgentsResponse, error) {
	client, _, ok, err := m.Client(id)
	if err != nil {
		return nil, AgentsResponse{}, err
	}
	if !ok || client == nil {
		return nil, AgentsResponse{}, ErrServerNotFound
	}
	return client.Test(ctx)
}

var ErrServerNotFound = errString("remote server not found")

type errString string

func (e errString) Error() string { return string(e) }
