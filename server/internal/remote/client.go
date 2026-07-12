package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"mindfs/server/internal/e2ee"

	"github.com/gorilla/websocket"
)

const (
	clientTimeout = 30 * time.Second
)

type Client struct {
	server Server
	http   *http.Client

	mu       sync.Mutex
	clientID string
	key      []byte
}

type RootInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	RootPath    string `json:"root_path,omitempty"`
}

type AgentsResponse struct {
	Agents []map[string]any `json:"agents"`
	Shells []map[string]any `json:"shells"`
}

type WSConn struct {
	conn *websocket.Conn
	key  []byte
}

func NewClient(server Server) *Client {
	return &Client{
		server: server,
		http: &http.Client{
			Timeout: clientTimeout,
		},
	}
}

func (c *Client) Server() Server {
	if c == nil {
		return Server{}
	}
	return c.server
}

func (c *Client) Test(ctx context.Context) ([]RootInfo, AgentsResponse, error) {
	var roots []RootInfo
	if err := c.JSON(ctx, http.MethodGet, "/api/dirs", nil, &roots); err != nil {
		return nil, AgentsResponse{}, err
	}
	var agents AgentsResponse
	if err := c.JSON(ctx, http.MethodGet, "/api/agents", nil, &agents); err != nil {
		return roots, AgentsResponse{}, err
	}
	return roots, agents, nil
}

func (c *Client) Agents(ctx context.Context) (AgentsResponse, error) {
	var out AgentsResponse
	if err := c.JSON(ctx, http.MethodGet, "/api/agents", nil, &out); err != nil {
		return AgentsResponse{}, err
	}
	return out, nil
}

func (c *Client) JSON(ctx context.Context, method, path string, body any, out any) error {
	if err := c.ensureSession(ctx); err != nil {
		return err
	}
	err := c.jsonOnce(ctx, method, path, body, out)
	if err == nil || !isRemoteAuthError(err) {
		return err
	}
	c.clearSession()
	if err := c.ensureSession(ctx); err != nil {
		return err
	}
	return c.jsonOnce(ctx, method, path, body, out)
}

func (c *Client) DialWS(ctx context.Context, path string) (*WSConn, error) {
	if err := c.ensureSession(ctx); err != nil {
		return nil, err
	}
	return c.dialWSOnce(ctx, path)
}

func (c *Client) jsonOnce(ctx context.Context, method, path string, body any, out any) error {
	key, clientID, err := c.session()
	if err != nil {
		return err
	}
	target, err := c.url(path)
	if err != nil {
		return err
	}
	var reqBody io.Reader
	if body != nil {
		envelope, err := e2ee.EncryptJSON(key, body)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("X-MindFS-E2EE", "1")
	req.Header.Set("X-MindFS-Client-ID", clientID)
	ts := time.Now().UTC().Format(time.RFC3339)
	req.Header.Set("X-MindFS-TS", ts)
	req.Header.Set("X-MindFS-Proof", e2ee.BuildRequestProof(key, method, requestProofPath(path), ts, clientID))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return remoteHTTPError{StatusCode: resp.StatusCode, Body: string(payload)}
	}
	if out == nil {
		return nil
	}
	if len(strings.TrimSpace(string(payload))) == 0 {
		return nil
	}
	var envelope e2ee.CipherEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return err
	}
	return e2ee.DecryptJSON(key, &envelope, out)
}

func (c *Client) dialWSOnce(ctx context.Context, path string) (*WSConn, error) {
	key, clientID, err := c.session()
	if err != nil {
		return nil, err
	}
	path = ensureLeadingSlash(path)
	params := url.Values{}
	params.Set("client_id", clientID)
	proofPath := path + "?" + params.Encode()
	ts := time.Now().UTC().Format(time.RFC3339)
	params.Set("e2ee_ts", ts)
	params.Set("e2ee_proof", e2ee.BuildRequestProof(key, http.MethodGet, proofPath, ts, clientID))
	target, err := c.wsURL(path + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	dialer := *websocket.DefaultDialer
	if strings.HasPrefix(strings.TrimSpace(c.server.BaseURL), "https://") {
		dialer.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	conn, resp, err := dialer.DialContext(ctx, target, nil)
	if err != nil {
		if resp != nil && resp.Body != nil {
			defer resp.Body.Close()
			payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			if len(payload) > 0 {
				return nil, fmt.Errorf("remote websocket failed: %s: %s", resp.Status, strings.TrimSpace(string(payload)))
			}
		}
		return nil, err
	}
	return &WSConn{conn: conn, key: append([]byte(nil), key...)}, nil
}

func (c *Client) ensureSession(ctx context.Context) error {
	c.mu.Lock()
	if len(c.key) > 0 && c.clientID != "" {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	if strings.TrimSpace(c.server.PairingSecret) == "" {
		return errors.New("remote pairing_secret required")
	}
	clientPriv, clientEphPK, err := e2ee.GenerateECDHKeypair()
	if err != nil {
		return err
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return err
	}
	clientNonce := base64.StdEncoding.EncodeToString(nonceBytes)
	clientID := "remote-" + c.server.ID + "-" + strconvTime(time.Now())
	body := map[string]string{
		"client_id":     clientID,
		"node_id":       c.server.NodeID,
		"client_eph_pk": clientEphPK,
		"client_nonce":  clientNonce,
		"proof":         e2ee.BuildOpenProof(c.server.PairingSecret, c.server.NodeID, clientEphPK, clientNonce),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	target, err := c.url("/api/e2ee/open")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respPayload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return remoteHTTPError{StatusCode: resp.StatusCode, Body: string(respPayload)}
	}
	var openResp struct {
		OK          bool   `json:"ok"`
		NodeEphPK   string `json:"node_eph_pk"`
		ServerNonce string `json:"server_nonce"`
		ServerProof string `json:"server_proof"`
	}
	if err := json.Unmarshal(respPayload, &openResp); err != nil {
		return err
	}
	expectedProof := e2ee.BuildAcceptProof(c.server.PairingSecret, c.server.NodeID, clientEphPK, openResp.NodeEphPK, clientNonce, openResp.ServerNonce)
	if !e2ee.VerifyProof(expectedProof, openResp.ServerProof) {
		return errors.New("remote e2ee accept proof invalid")
	}
	serverPub, err := e2ee.DecodePublicKey(openResp.NodeEphPK)
	if err != nil {
		return err
	}
	derived, err := e2ee.DeriveKey(c.server.PairingSecret, c.server.NodeID, clientEphPK, openResp.NodeEphPK, clientNonce, openResp.ServerNonce, clientPriv, serverPub)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.clientID = clientID
	c.key = append([]byte(nil), derived.Transport...)
	c.mu.Unlock()
	return nil
}

func (c *Client) session() ([]byte, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.key) == 0 || c.clientID == "" {
		return nil, "", errors.New("remote e2ee session missing")
	}
	return append([]byte(nil), c.key...), c.clientID, nil
}

func (c *Client) clearSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.key {
		c.key[i] = 0
	}
	c.key = nil
	c.clientID = ""
}

func (c *Client) url(path string) (string, error) {
	path = ensureLeadingSlash(path)
	base := strings.TrimRight(c.server.BaseURL, "/")
	return base + path, nil
}

func (c *Client) wsURL(path string) (string, error) {
	target, err := url.Parse(strings.TrimRight(c.server.BaseURL, "/") + ensureLeadingSlash(path))
	if err != nil {
		return "", err
	}
	switch target.Scheme {
	case "http":
		target.Scheme = "ws"
	case "https":
		target.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported remote scheme: %s", target.Scheme)
	}
	return target.String(), nil
}

func (w *WSConn) SendJSON(value any) error {
	if w == nil || w.conn == nil {
		return errors.New("remote websocket not connected")
	}
	envelope, err := e2ee.EncryptJSON(w.key, value)
	if err != nil {
		return err
	}
	return w.conn.WriteJSON(envelope)
}

func (w *WSConn) ReadJSON(out any) error {
	if w == nil || w.conn == nil {
		return errors.New("remote websocket not connected")
	}
	_, payload, err := w.conn.ReadMessage()
	if err != nil {
		return err
	}
	var envelope e2ee.CipherEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return err
	}
	return e2ee.DecryptJSON(w.key, &envelope, out)
}

func (w *WSConn) Close() error {
	if w == nil || w.conn == nil {
		return nil
	}
	return w.conn.Close()
}

type remoteHTTPError struct {
	StatusCode int
	Body       string
}

func (e remoteHTTPError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("remote http status %d", e.StatusCode)
	}
	return fmt.Sprintf("remote http status %d: %s", e.StatusCode, body)
}

func isRemoteAuthError(err error) bool {
	var httpErr remoteHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden
	}
	return false
}

func requestProofPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return "/"
	}
	return ensureLeadingSlash(path)
}

func ensureLeadingSlash(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func strconvTime(t time.Time) string {
	return fmt.Sprintf("%d", t.UnixNano())
}
