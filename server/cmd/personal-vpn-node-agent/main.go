package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jeni0101/vpn/server/internal/nodeagent"
)

func main() {
	logger := log.New(os.Stderr, "personal-vpn-node-agent: ", log.LstdFlags|log.LUTC)
	interval, err := time.ParseDuration(env("TNEST_NODE_POLL_INTERVAL", "30s"))
	if err != nil {
		logger.Fatal("invalid node poll interval")
	}
	apply, _ := strconv.ParseBool(env("TNEST_NODE_APPLY_CHANGES", "false"))
	agent, err := nodeagent.New(nodeagent.Config{
		ControlURL: env("TNEST_CONTROL_URL", "https://vpn.tnestai.asia"),
		NodeID: env("TNEST_NODE_ID", ""),
		TokenPath: env("TNEST_NODE_API_TOKEN_FILE", "/etc/personal-vpn-node/api.token"),
		ClientCertPath: env("TNEST_NODE_CLIENT_CERT", "/etc/personal-vpn-node/client.crt"),
		ClientKeyPath: env("TNEST_NODE_CLIENT_KEY", "/etc/personal-vpn-node/client.key"),
		PrivateKeyPath: env("TNEST_WG_PRIVATE_KEY", "/etc/wireguard/wg0.key"),
		Interface: env("TNEST_WG_INTERFACE", "wg0"),
		RuntimeDir: env("TNEST_NODE_RUNTIME_DIR", "/run/personal-vpn-node"),
		BackupDir: env("TNEST_NODE_BACKUP_DIR", "/var/lib/personal-vpn-node/backups"),
		Interval: interval, ApplyChanges: apply,
	}, logger)
	if err != nil {
		logger.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM,
	)
	defer stop()
	agent.Run(ctx)
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
