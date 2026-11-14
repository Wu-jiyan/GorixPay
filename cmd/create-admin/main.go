// Command create-admin creates the first operator account.
//
//	go run ./cmd/create-admin --username admin --password 'a-long-password' \
//	  --database-driver mysql \
//	  --database-dsn 'gorix:secret@tcp(127.0.0.1:3306)/gorix?charset=utf8mb4&parseTime=True&loc=UTC'
//
// It applies the migrations first, so it also works against an empty database, and
// it is idempotent: running it again with the same username resets the password
// instead of creating a second account. The password is read from the flag or, when
// the flag is omitted, from stdin so it never has to appear in shell history.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"gorixpay/internal/identity/seed"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/migrate"
	"gorixpay/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "create-admin:", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("create-admin", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	username := flags.String("username", "admin", "administrator username")
	password := flags.String("password", "", "administrator password (omit to read from stdin)")
	displayName := flags.String("display-name", "", "display name, defaults to the username")
	driver := flags.String("database-driver", envOr("GORIXPAY_DATABASE_DRIVER", "sqlite"), "sqlite, mysql or postgres")
	dsn := flags.String("database-dsn", envOr("GORIXPAY_DATABASE_DSN", "gorixpay.db"), "database DSN")
	skipMigrate := flags.Bool("skip-migrate", false, "do not apply migrations before creating the account")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	value := *password
	if value == "" {
		read, err := readPassword()
		if err != nil {
			return err
		}
		value = read
	}
	if len(value) < seed.MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", seed.MinPasswordLength)
	}

	ctx := context.Background()
	db, _, err := database.Open(ctx, config.DatabaseConfig{Driver: *driver, DSN: *dsn})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer func() { _ = sqlDB.Close() }()
	}
	if !*skipMigrate {
		if err := (migrate.Runner{DB: db, FS: migrations.Files}).Up(ctx); err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
	}
	created, err := seed.EnsureAdministrator(ctx, db, seed.Administrator{
		Username: *username, Password: value, DisplayName: *displayName,
	})
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("administrator %q created\n", strings.TrimSpace(*username))
		return nil
	}
	fmt.Printf("administrator %q already existed, password updated\n", strings.TrimSpace(*username))
	return nil
}

// readPassword reads one line from stdin. The terminal echo is not suppressed (that
// would need a termios dependency); piping is the recommended way:
//
//	printf '%s' "$PASSWORD" | create-admin --username admin
func readPassword() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
