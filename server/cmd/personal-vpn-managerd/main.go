package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"

	"github.com/jeni0101/vpn/server/internal/config"
	"github.com/jeni0101/vpn/server/internal/catalog"
	"github.com/jeni0101/vpn/server/internal/manager"
	"github.com/jeni0101/vpn/server/internal/security"
	"github.com/jeni0101/vpn/server/internal/store"
)

func main() {
	logger := log.New(os.Stderr, "personal-vpn-managerd: ", log.LstdFlags|log.LUTC)
	cfg, err := config.ManagerFromEnv()
	if err != nil {
		logger.Fatal(err)
	}
	if cfg.ServerPublicKey == "" {
		client, err := wgctrl.New()
		if err != nil {
			logger.Fatal(err)
		}
		device, err := client.Device(cfg.WGInterface)
		client.Close()
		if err != nil {
			logger.Fatal(err)
		}
		cfg.ServerPublicKey = device.PublicKey.String()
	}
	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		logger.Fatal(err)
	}
	defer db.Close()
	sealer, err := security.LoadOrCreateKey(cfg.MasterKeyPath)
	if err != nil {
		logger.Fatal(err)
	}
	wg := &manager.KernelWireGuard{
		Interface: cfg.WGInterface, ConfigPath: cfg.WGConfigPath,
		ApplyLive: cfg.ApplyChanges, BackupRecipient: cfg.BackupRecipient,
	}
	service := manager.NewService(cfg, db, sealer, wg)
	catalogSigner, err := catalog.LoadOrCreateSigner(cfg.CatalogSigningKeyPath)
	if err != nil {
		logger.Fatal(err)
	}
	service.SetCatalogSigner(catalogSigner)
	if err := service.EnsureLocalNode(context.Background()); err != nil {
		logger.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0750); err != nil {
		logger.Fatal(err)
	}
	if err := removeSocket(cfg.SocketPath); err != nil {
		logger.Fatal(err)
	}
	listener, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		logger.Fatal(err)
	}
	defer func() {
		listener.Close()
		_ = os.Remove(cfg.SocketPath)
	}()
	if err := os.Chmod(cfg.SocketPath, 0660); err != nil {
		logger.Fatal(err)
	}
	server := &http.Server{
		Handler:           manager.NewHTTPServer(service),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go service.Poll(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	logger.Printf("listening on %s (apply=%t)", cfg.SocketPath, cfg.ApplyChanges)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal(err)
	}
}

func removeSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("manager socket path exists and is not a socket")
	}
	return os.Remove(path)
}
