// Command yasssm (Yet Another Simple Self-hosted Server Manager) is a small server-rendered
// web UI for managing one host:
// Docker Compose stacks, containers, systemd services, cron and system stats.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

type config struct {
	Addr       string
	StacksDir  string
	TOTPFile   string
	SessionTTL time.Duration
	// SecureCookie marks the session cookie Secure; set it when a TLS proxy
	// (tailscale serve) sits in front and yasssm itself speaks plain HTTP.
	SecureCookie bool
}

func loadConfig() (config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return config{}, err
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return config{}, err
	}
	return config{
		Addr:         envOr("YASSSM_ADDR", "127.0.0.1:8090"),
		StacksDir:    envOr("YASSSM_STACKS_DIR", home),
		TOTPFile:     envOr("YASSSM_TOTP_FILE", filepath.Join(cfgDir, "yasssm", "totp")),
		SessionTTL:   12 * time.Hour,
		SecureCookie: os.Getenv("YASSSM_SECURE_COOKIE") == "1",
	}, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "totp-init":
		err = totpInit(args)
	case "totp-qr":
		err = totpQR()
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprintln(os.Stderr, "usage: yasssm [serve | totp-init [-force] | totp-qr | version]")
		os.Exit(2)
	}
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	tp, err := newTOTP(cfg.TOTPFile)
	if err != nil {
		return fmt.Errorf("%w (run `yasssm totp-init` first)", err)
	}
	a := newApp(cfg, tp)
	srv := &http.Server{Addr: cfg.Addr, Handler: a.routes(), ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr, "stacks_dir", cfg.StacksDir, "version", version)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}

func totpInit(args []string) error {
	fs := flag.NewFlagSet("totp-init", flag.ContinueOnError)
	force := fs.Bool("force", false, "replace an existing secret")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if _, err := os.Stat(cfg.TOTPFile); err == nil && !*force {
		return fmt.Errorf("%s already exists; pass -force to replace it", cfg.TOTPFile)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	key := make([]byte, 20)
	rand.Read(key)
	secret := totpEncoding.EncodeToString(key)
	if err := os.MkdirAll(filepath.Dir(cfg.TOTPFile), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(cfg.TOTPFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(secret + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	fmt.Printf("Secret saved to %s\n\n", cfg.TOTPFile)
	if err := printEnrollment(secret); err != nil {
		return err
	}
	fmt.Println("\nA running yasssm picks up the new secret on the next login.")
	return nil
}

// totpQR shows the existing secret again, e.g. to enroll another device.
func totpQR() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if _, err := loadTOTPKey(cfg.TOTPFile); err != nil {
		return fmt.Errorf("%w (run `yasssm totp-init` first)", err)
	}
	b, err := os.ReadFile(cfg.TOTPFile)
	if err != nil {
		return err
	}
	return printEnrollment(strings.TrimSpace(string(b)))
}

func printEnrollment(secret string) error {
	host, _ := os.Hostname()
	uri := (&url.URL{
		Scheme:   "otpauth",
		Host:     "totp",
		Path:     "/yasssm:" + host,
		RawQuery: url.Values{"secret": {secret}, "issuer": {"yasssm"}}.Encode(),
	}).String()
	fmt.Printf("Scan with your authenticator app (TOTP, SHA1, 6 digits, 30s):\n\n")
	if err := writeQR(os.Stdout, uri); err != nil {
		return err
	}
	fmt.Printf("\nOr enter it by hand:\n  secret: %s\n  uri:    %s\n", secret, uri)
	return nil
}
