package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jeni0101/vpn/server/internal/auth"
	"github.com/jeni0101/vpn/server/internal/config"
	"github.com/jeni0101/vpn/server/internal/managerclient"
	"github.com/jeni0101/vpn/server/internal/security"
	"github.com/jeni0101/vpn/server/internal/webapp"
)

func main() {
	logger := log.New(os.Stderr, "personal-vpn-web: ", log.LstdFlags|log.LUTC)
	cfg, err := config.WebFromEnv()
	if err != nil {
		logger.Fatal(err)
	}
	sealer, err := security.LoadOrCreateKey(cfg.AuthKeyPath)
	if err != nil {
		logger.Fatal(err)
	}
	authStore, err := auth.Open(cfg.AuthDBPath, sealer)
	if err != nil {
		logger.Fatal(err)
	}
	defer authStore.Close()

	if len(os.Args) > 1 && os.Args[1] == "init-admin" {
		initializeAdmin(logger, authStore, os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] != "serve" {
		logger.Fatalf("unknown command %q", os.Args[1])
	}

	managerClient := managerclient.New(cfg.ManagerSocket)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := managerClient.Health(ctx); err != nil {
		cancel()
		logger.Fatalf("manager is unavailable: %v", err)
	}
	cancel()
	app, err := webapp.New(authStore, managerClient, logger, cfg.SecureCookies,
		cfg.PublicDir, cfg.ReleasesPath, cfg.NodeAPIToken)
	if err != nil {
		logger.Fatal(err)
	}
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	logger.Printf("listening on %s", cfg.ListenAddress)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal(err)
	}
}

func initializeAdmin(logger *log.Logger, store *auth.Store, args []string) {
	flags := flag.NewFlagSet("init-admin", flag.ExitOnError)
	username := flags.String("username", "admin", "administrator username")
	passwordStdin := flags.Bool("password-stdin", false, "read one password line from standard input")
	_ = flags.Parse(args)
	if !*passwordStdin {
		logger.Fatal("init-admin requires --password-stdin")
	}
	reader := bufio.NewReader(os.Stdin)
	password, err := reader.ReadString('\n')
	if err != nil && len(password) == 0 {
		logger.Fatal("read password: ", err)
	}
	password = strings.TrimRight(password, "\r\n")
	result, err := store.Initialize(context.Background(), *username, password)
	for i := range password {
		_ = i
	}
	if err != nil {
		logger.Fatal(err)
	}
	fmt.Printf("ADMIN=%s\nTOTP_URI=%s\nRECOVERY_CODES=\n", result.Username, result.ProvisionURI)
	for _, code := range result.RecoveryCodes {
		fmt.Println(code)
	}
}
