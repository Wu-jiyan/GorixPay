package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
	"gorixpay/internal/platform/app"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/logging"
	"gorixpay/internal/platform/migrate"
	"gorixpay/migrations"

	"github.com/google/uuid"
	"golang.org/x/term"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) >= 2 && args[0] == "admin" && args[1] == "create" {
		return runAdminCreate(args[2:])
	}
	cfg, err := config.Load(args, os.Getenv)
	if err != nil {
		return 2
	}
	logger := logging.New(os.Stdout, slog.LevelInfo)
	application, err := app.New(context.Background(), cfg, logger)
	if err != nil {
		logger.Error("initialize application", "error", err)
		return 1
	}
	defer application.Close()
	server := &http.Server{
		Addr:              application.Address(),
		Handler:           application.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("serve HTTP", "error", err)
			return 1
		}
	case <-signals:
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("shutdown HTTP", "error", err)
			return 1
		}
	}
	return 0
}

func runAdminCreate(args []string) int {
	commandArgs, configArgs, err := splitAdminCreateArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	flags := flag.NewFlagSet("admin create", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	username := flags.String("username", "", "administrator username")
	displayName := flags.String("display-name", "", "administrator display name")
	if err := flags.Parse(commandArgs); err != nil {
		return 2
	}
	*username = strings.TrimSpace(*username)
	*displayName = strings.TrimSpace(*displayName)
	if *username == "" || *displayName == "" {
		fmt.Fprintln(os.Stderr, "--username and --display-name are required")
		return 2
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "password input requires an interactive terminal")
		return 2
	}
	fmt.Fprint(os.Stderr, "Password: ")
	plain, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read password:", err)
		return 1
	}
	fmt.Fprint(os.Stderr, "Confirm password: ")
	confirmation, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read password confirmation:", err)
		return 1
	}
	if string(plain) != string(confirmation) {
		fmt.Fprintln(os.Stderr, "passwords do not match")
		return 2
	}
	hash, err := password.Hash(string(plain))
	for index := range plain {
		plain[index] = 0
	}
	for index := range confirmation {
		confirmation[index] = 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hash password:", err)
		return 1
	}
	cfg, err := config.Load(configArgs, os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx := context.Background()
	db, _, err := database.Open(ctx, cfg.Database)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open database:", err)
		return 1
	}
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}
	if err := (migrate.Runner{DB: db, FS: migrations.Files}).Up(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "migrate database:", err)
		return 1
	}
	now := time.Now().UTC()
	administrator := identitymodel.Administrator{ID: uuid.NewString(), Username: *username, PasswordHash: hash, DisplayName: *displayName, Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.WithContext(ctx).Create(&administrator).Error; err != nil {
		fmt.Fprintln(os.Stderr, "create administrator:", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, administrator.ID)
	return 0
}

func splitAdminCreateArgs(args []string) ([]string, []string, error) {
	commandArgs := make([]string, 0, 4)
	configArgs := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		value := args[index]
		if value == "--username" || value == "--display-name" {
			if index+1 >= len(args) {
				return nil, nil, errors.New(value + " requires a value")
			}
			commandArgs = append(commandArgs, value, args[index+1])
			index++
			continue
		}
		if strings.HasPrefix(value, "--username=") || strings.HasPrefix(value, "--display-name=") {
			commandArgs = append(commandArgs, value)
			continue
		}
		configArgs = append(configArgs, value)
	}
	return commandArgs, configArgs, nil
}
