package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Manager struct {
	SocketPath      string
	DatabasePath    string
	MasterKeyPath   string
	WGConfigPath    string
	WGInterface     string
	ManagementURL   string
	Endpoint        string
	ServerPublicKey string
	BackupRecipient string
	ApplyChanges    bool
	PollInterval    time.Duration
	Quarantine      time.Duration
	InviteTTL       time.Duration
}

type Web struct {
	ListenAddress string
	ManagerSocket string
	AuthDBPath    string
	AuthKeyPath   string
	PublicDir     string
	ReleasesPath  string
	SecureCookies bool
}

func ManagerFromEnv() (Manager, error) {
	cfg := Manager{
		SocketPath:      env("TNEST_MANAGER_SOCKET", "/run/personal-vpn/manager.sock"),
		DatabasePath:    env("TNEST_MANAGER_DB", "/var/lib/personal-vpn/manager.db"),
		MasterKeyPath:   env("TNEST_MANAGER_KEY", "/etc/personal-vpn/manager.key"),
		WGConfigPath:    env("TNEST_WG_CONFIG", "/etc/wireguard/wg0.conf"),
		WGInterface:     env("TNEST_WG_INTERFACE", "wg0"),
		ManagementURL:   env("TNEST_MANAGEMENT_URL", "https://vpn.example.com"),
		Endpoint:        env("TNEST_WG_ENDPOINT", "203.0.113.10:51999"),
		ServerPublicKey: os.Getenv("TNEST_SERVER_PUBLIC_KEY"),
		BackupRecipient: os.Getenv("TNEST_AGE_RECIPIENT"),
		ApplyChanges:    envBool("TNEST_APPLY_CHANGES", false),
		PollInterval:    30 * time.Second,
		Quarantine:      7 * 24 * time.Hour,
		InviteTTL:       10 * time.Minute,
	}
	if value := os.Getenv("TNEST_POLL_INTERVAL"); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil {
			return Manager{}, fmt.Errorf("TNEST_POLL_INTERVAL: %w", err)
		}
		cfg.PollInterval = d
	}
	if err := validateManager(cfg); err != nil {
		return Manager{}, err
	}
	return cfg, nil
}

func WebFromEnv() (Web, error) {
	cfg := Web{
		ListenAddress: env("TNEST_WEB_LISTEN", "127.0.0.1:8787"),
		ManagerSocket: env("TNEST_MANAGER_SOCKET", "/run/personal-vpn/manager.sock"),
		AuthDBPath:    env("TNEST_AUTH_DB", "/var/lib/personal-vpn-web/auth.db"),
		AuthKeyPath:   env("TNEST_AUTH_KEY", "/etc/personal-vpn-web/auth.key"),
		PublicDir:     os.Getenv("TNEST_WEB_PUBLIC"),
		ReleasesPath:  env("TNEST_RELEASES_FILE", "/var/lib/personal-vpn-web/releases.json"),
		SecureCookies: envBool("TNEST_SECURE_COOKIES", true),
	}
	if cfg.ListenAddress == "" || cfg.ManagerSocket == "" {
		return Web{}, errors.New("web listen address and manager socket are required")
	}
	return cfg, nil
}

func validateManager(cfg Manager) error {
	if cfg.WGInterface != "wg0" {
		return errors.New("only wg0 is supported")
	}
	u, err := url.Parse(cfg.ManagementURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" {
		return errors.New("management URL must be an HTTPS origin without a path")
	}
	if cfg.Endpoint == "" {
		return errors.New("WireGuard endpoint is required")
	}
	for _, path := range []string{cfg.SocketPath, cfg.DatabasePath, cfg.MasterKeyPath, cfg.WGConfigPath} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("path must be absolute: %s", path)
		}
	}
	if cfg.PollInterval < time.Second {
		return errors.New("poll interval must be at least one second")
	}
	return nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
