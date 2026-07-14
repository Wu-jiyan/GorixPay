package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesDefaults(t *testing.T) {
	cfg, err := Load(nil, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Address != ":8080" || cfg.HTTP.PublicURL != "http://localhost:8080" || cfg.Database.Driver != "sqlite" || cfg.Database.DSN != "gorixpay.db" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadPrecedence(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.yaml")
	data := []byte("http:\n  address: ':7000'\ndatabase:\n  driver: mysql\n  dsn: file-dsn\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"GORIXPAY_HTTP_ADDRESS":    ":8000",
		"GORIXPAY_PUBLIC_URL":      "https://env.example",
		"GORIXPAY_DATABASE_DRIVER": "postgres",
		"GORIXPAY_DATABASE_DSN":    "env-dsn",
	}
	cfg, err := Load([]string{"--config", path, "--http-address", ":9000", "--public-url", "https://flag.example", "--database-dsn", "flag-dsn"}, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Address != ":9000" || cfg.HTTP.PublicURL != "https://flag.example" || cfg.Database.Driver != "postgres" || cfg.Database.DSN != "flag-dsn" {
		t.Fatalf("unexpected precedence result: %+v", cfg)
	}
}

func TestLoadRejectsInvalidDriverAndMissingProductionMasterKey(t *testing.T) {
	if _, err := Load([]string{"--database-driver", "oracle"}, func(string) string { return "" }); err == nil {
		t.Fatal("invalid database driver accepted")
	}
	if _, err := Load([]string{"--environment", "production"}, func(string) string { return "" }); err == nil {
		t.Fatal("production configuration without master key accepted")
	}
}
