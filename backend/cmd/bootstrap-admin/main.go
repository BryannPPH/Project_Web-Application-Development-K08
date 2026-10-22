package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/config"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/platformdb"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap admin failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	username, password, err := readCredentials()
	if err != nil {
		return err
	}
	// Hapus salinan kata sandi dari environment secepatnya.
	_ = os.Unsetenv("BOOTSTRAP_ADMIN_PASSWORD")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	database, err := platformdb.Open(ctx, cfg.PlatformDatabase)
	if err != nil {
		return err
	}
	defer database.Close()

	bootstrapper := auth.NewBootstrapper(database, auth.NewArgon2idHasher())
	if err := bootstrapper.CreateInitialAdmin(ctx, username, password); err != nil {
		if errors.Is(err, auth.ErrPlatformAdminExists) {
			return err
		}
		return fmt.Errorf("create initial admin: %w", err)
	}
	fmt.Printf("initial platform admin %q created\n", strings.TrimSpace(username))
	return nil
}

func readCredentials() (string, string, error) {
	username := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_USERNAME"))
	if username == "" {
		fmt.Fprint(os.Stderr, "Admin username: ")
		value, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return "", "", fmt.Errorf("read admin username: %w", err)
		}
		username = strings.TrimSpace(value)
	}

	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if password != "" {
		return username, password, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", "", errors.New("BOOTSTRAP_ADMIN_PASSWORD is required in non-interactive mode")
	}

	fmt.Fprint(os.Stderr, "Admin password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", "", fmt.Errorf("read admin password: %w", err)
	}
	fmt.Fprint(os.Stderr, "Confirm password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", "", fmt.Errorf("confirm admin password: %w", err)
	}
	if string(first) != string(second) {
		return "", "", errors.New("password confirmation does not match")
	}
	return username, string(first), nil
}
