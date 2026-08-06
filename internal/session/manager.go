package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	peermgr "torrent-demo/internal/peer_manager"
)

// SessionManager is a registry of sessions. Its mutex protects only the map
// itself; each Session guards its own internals.
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	options  Options
}

func NewSessionManager(options Options) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
		options:  options,
	}
}

// Add parses the torrent eagerly and registers the session as Pending.
func (m *SessionManager) Add(torrentPath string) (SessionSummary, error) {
	id, err := newSessionID()
	if err != nil {
		return SessionSummary{}, fmt.Errorf("failed to generate session id: %w", err)
	}
	s, err := NewTorrentSession(id, torrentPath, m.options)
	if err != nil {
		return SessionSummary{}, err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	return s.Summary(), nil
}

func (m *SessionManager) Start(ctx context.Context, id string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	return s.Start(ctx)
}

func (m *SessionManager) Stop(id string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	s.Stop()
	return nil
}

// Remove deletes a session from the registry. Rejected while Running:
// Start() isn't safely re-callable after Stop() today (the done channel
// closes once), so removal is a one-way deletion, not a restart primitive.
func (m *SessionManager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("unknown session %q", id)
	}
	if s.Status() == StatusRunning {
		return fmt.Errorf("session %q is running; stop it first", id)
	}
	delete(m.sessions, id)
	return nil
}

func (m *SessionManager) List() []SessionSummary {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SessionSummary, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Summary())
	}
	// Deterministic order for a stable UI; map iteration is random.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (m *SessionManager) Peers(id string) ([]peermgr.PeerSnapshot, error) {
	s, err := m.get(id)
	if err != nil {
		return nil, err
	}
	return s.Peers(), nil
}

func (m *SessionManager) get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("unknown session %q", id)
	}
	return s, nil
}

// newSessionID returns an 8-byte cryptographically random id, hex-encoded.
func newSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
