package nodeagent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/jeni0101/vpn/server/internal/model"
)

type Config struct {
	ControlURL     string
	NodeID         string
	TokenPath      string
	ClientCertPath string
	ClientKeyPath  string
	PrivateKeyPath string
	Interface      string
	RuntimeDir     string
	BackupDir      string
	Interval       time.Duration
	ApplyChanges   bool
}

type Agent struct {
	cfg         Config
	client      *http.Client
	logger      *log.Logger
	token       string
	lastVersion int64
}

func New(cfg Config, logger *log.Logger) (*Agent, error) {
	if cfg.ControlURL == "" || cfg.NodeID == "" || cfg.TokenPath == "" ||
		cfg.ClientCertPath == "" || cfg.ClientKeyPath == "" ||
		cfg.PrivateKeyPath == "" || cfg.Interface != "wg0" {
		return nil, errors.New("invalid node agent configuration")
	}
	if cfg.Interval < 5*time.Second {
		return nil, errors.New("node poll interval must be at least five seconds")
	}
	tokenBytes, err := os.ReadFile(cfg.TokenPath)
	if err != nil {
		return nil, fmt.Errorf("read node API token: %w", err)
	}
	tokenValue := strings.TrimSpace(string(tokenBytes))
	if len(tokenValue) < 32 {
		return nil, errors.New("invalid node API token")
	}
	certificate, err := tls.LoadX509KeyPair(cfg.ClientCertPath, cfg.ClientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load node mTLS identity: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
	}
	return &Agent{
		cfg: cfg, logger: logger, token: tokenValue,
		client: &http.Client{Transport: transport, Timeout: 20 * time.Second},
	}, nil
}

func (a *Agent) Run(ctx context.Context) {
	a.syncOnce(ctx)
	ticker := time.NewTicker(a.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.syncOnce(ctx)
		}
	}
}

func (a *Agent) syncOnce(ctx context.Context) {
	state, err := a.desiredState(ctx)
	if err != nil {
		a.logger.Printf("desired state unavailable: %v", redactedError(err))
		a.report(ctx, false, 0, "desired state unavailable")
		return
	}
	if state.NodeID != a.cfg.NodeID {
		a.logger.Printf("rejected mismatched node identity")
		a.report(ctx, false, 0, "node identity mismatch")
		return
	}
	if state.Version != a.lastVersion {
		if !a.cfg.ApplyChanges {
			a.logger.Printf(
				"validated desired state version=%d peers=%d (read-only)",
				state.Version, len(state.Peers),
			)
		} else if err := a.apply(ctx, state); err != nil {
			a.logger.Printf("peer synchronization failed: %v", redactedError(err))
			a.report(ctx, false, len(state.Peers), "peer synchronization failed")
			return
		}
		a.lastVersion = state.Version
	}
	a.report(ctx, true, len(state.Peers), "")
}

func (a *Agent) desiredState(ctx context.Context) (model.NodeDesiredState, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		strings.TrimRight(a.cfg.ControlURL, "/")+"/api/v2/node/desired-state",
		nil,
	)
	if err != nil {
		return model.NodeDesiredState{}, err
	}
	request.Header.Set("Authorization", "Bearer "+a.token)
	response, err := a.client.Do(request)
	if err != nil {
		return model.NodeDesiredState{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return model.NodeDesiredState{}, err
	}
	if response.StatusCode != http.StatusOK {
		return model.NodeDesiredState{}, fmt.Errorf("control plane returned HTTP %d", response.StatusCode)
	}
	var state model.NodeDesiredState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return model.NodeDesiredState{}, errors.New("invalid desired state")
	}
	if err := validateState(state); err != nil {
		return model.NodeDesiredState{}, err
	}
	return state, nil
}

func validateState(state model.NodeDesiredState) error {
	if state.Version <= 0 || state.NodeID == "" || state.RegionCode == "" ||
		state.ListenPort < 1 || state.ListenPort > 65535 ||
		(state.ExitMode != model.ExitModeDualStack &&
			state.ExitMode != model.ExitModeIPv4BlockIPv6) {
		return errors.New("invalid desired state")
	}
	seen := make(map[string]bool, len(state.Peers))
	for _, peer := range state.Peers {
		if peer.DeviceID == "" || peer.PublicKey == "" || peer.PresharedKey == "" ||
			peer.IPv4 == "" || peer.IPv6 == "" || seen[peer.PublicKey] {
			return errors.New("invalid desired peer")
		}
		if _, err := wgtypes.ParseKey(peer.PublicKey); err != nil {
			return errors.New("invalid desired peer public key")
		}
		if _, err := wgtypes.ParseKey(peer.PresharedKey); err != nil {
			return errors.New("invalid desired peer preshared key")
		}
		seen[peer.PublicKey] = true
	}
	return nil
}

func (a *Agent) apply(ctx context.Context, state model.NodeDesiredState) error {
	privateBytes, err := os.ReadFile(a.cfg.PrivateKeyPath)
	if err != nil {
		return fmt.Errorf("read local WireGuard key: %w", err)
	}
	privateKey := strings.TrimSpace(string(privateBytes))
	for index := range privateBytes {
		privateBytes[index] = 0
	}
	if _, err := wgtypes.ParseKey(privateKey); err != nil {
		return errors.New("invalid local WireGuard key")
	}
	config := renderSyncConfig(privateKey, state)
	defer zero(config)
	if err := os.MkdirAll(a.cfg.RuntimeDir, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(a.cfg.BackupDir, 0700); err != nil {
		return err
	}
	backup, err := exec.CommandContext(ctx, "wg", "showconf", a.cfg.Interface).Output()
	if err != nil {
		return fmt.Errorf("capture WireGuard rollback point: %w", err)
	}
	defer zero(backup)
	backupPath := filepath.Join(
		a.cfg.BackupDir,
		fmt.Sprintf("%s-%s.conf", a.cfg.Interface, time.Now().UTC().Format("20060102T150405.000000000Z")),
	)
	if err := os.WriteFile(backupPath, backup, 0600); err != nil {
		return fmt.Errorf("write WireGuard rollback point: %w", err)
	}
	if err := a.syncConfig(ctx, config); err != nil {
		if rollbackErr := a.syncConfig(ctx, backup); rollbackErr != nil {
			return fmt.Errorf("apply failed and rollback failed: %w", err)
		}
		return err
	}
	a.pruneBackups()
	a.logger.Printf("applied desired state version=%d peers=%d", state.Version, len(state.Peers))
	return nil
}

func (a *Agent) syncConfig(ctx context.Context, config []byte) error {
	file, err := os.CreateTemp(a.cfg.RuntimeDir, ".wg-sync-*")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(config); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	output, err := exec.CommandContext(
		ctx, "wg", "syncconf", a.cfg.Interface, path,
	).CombinedOutput()
	zero(output)
	if err != nil {
		return errors.New("wg syncconf failed")
	}
	return nil
}

func renderSyncConfig(privateKey string, state model.NodeDesiredState) []byte {
	var builder strings.Builder
	fmt.Fprintf(&builder, "[Interface]\nPrivateKey = %s\nListenPort = %d\n\n",
		privateKey, state.ListenPort)
	peers := append([]model.DesiredPeer(nil), state.Peers...)
	sort.Slice(peers, func(i, j int) bool { return peers[i].DeviceID < peers[j].DeviceID })
	for _, peer := range peers {
		fmt.Fprintf(&builder,
			"[Peer]\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = %s/32, %s/128\n\n",
			peer.PublicKey, peer.PresharedKey, peer.IPv4, peer.IPv6,
		)
	}
	return []byte(builder.String())
}

func (a *Agent) report(ctx context.Context, healthy bool, peers int, lastError string) {
	report := model.NodeReport{
		NodeID: a.cfg.NodeID, Version: fmt.Sprintf("%d", a.lastVersion),
		Healthy: healthy, PeerCount: peers, LastError: lastError,
		ReportedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(report)
	if err != nil {
		return
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		strings.TrimRight(a.cfg.ControlURL, "/")+"/api/v2/node/report",
		bytes.NewReader(data),
	)
	if err != nil {
		return
	}
	request.Header.Set("Authorization", "Bearer "+a.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(request)
	if err == nil {
		io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		response.Body.Close()
	}
}

func (a *Agent) pruneBackups() {
	entries, err := os.ReadDir(a.cfg.BackupDir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	prefix := a.cfg.Interface + "-"
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) &&
			strings.HasSuffix(entry.Name(), ".conf") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names[:max(0, len(names)-10)] {
		_ = os.Remove(filepath.Join(a.cfg.BackupDir, name))
	}
}

func redactedError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 160 {
		value = value[:160]
	}
	return value
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
