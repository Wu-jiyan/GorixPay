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

func TestLoadDefaultsDisableRedisAndEnableSandboxInDevelopment(t *testing.T) {
	cfg, err := Load(nil, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Redis.Addr != "" || cfg.Redis.Password != "" || cfg.Redis.DB != 0 || cfg.Redis.Prefix != "" {
		t.Fatalf("unexpected redis defaults: %+v", cfg.Redis)
	}
	if !cfg.Sandbox.Enabled {
		t.Fatalf("sandbox should default to enabled in development: %+v", cfg.Sandbox)
	}
}

func TestLoadRedisAndSandboxFromEnvironment(t *testing.T) {
	env := map[string]string{
		"GORIXPAY_REDIS_ADDR":      "127.0.0.1:6379",
		"GORIXPAY_REDIS_PASSWORD":  "s3cret",
		"GORIXPAY_REDIS_DB":        "3",
		"GORIXPAY_REDIS_PREFIX":    "gp",
		"GORIXPAY_SANDBOX_ENABLED": "false",
	}
	cfg, err := Load(nil, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Redis.Addr != "127.0.0.1:6379" || cfg.Redis.Password != "s3cret" || cfg.Redis.DB != 3 || cfg.Redis.Prefix != "gp" {
		t.Fatalf("unexpected redis config: %+v", cfg.Redis)
	}
	if cfg.Sandbox.Enabled {
		t.Fatal("sandbox environment override ignored")
	}
}

func TestLoadRedisAndSandboxFlagsOverrideEnvironment(t *testing.T) {
	env := map[string]string{"GORIXPAY_REDIS_ADDR": "env:6379"}
	cfg, err := Load([]string{"--redis-addr", "flag:6380", "--sandbox-enabled=false"}, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Redis.Addr != "flag:6380" {
		t.Fatalf("unexpected redis addr: %q", cfg.Redis.Addr)
	}
	if cfg.Sandbox.Enabled {
		t.Fatal("sandbox flag override ignored")
	}
}

func TestLoadProductionForcesSandboxOff(t *testing.T) {
	cfg, err := Load([]string{"--environment", "production", "--master-key-file", "master.key", "--sandbox-enabled=true"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sandbox.Enabled {
		t.Fatalf("production enabled sandbox: %+v", cfg.Sandbox)
	}
}

func TestValidateRejectsNegativeRedisDatabase(t *testing.T) {
	cfg, err := Load(nil, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	cfg.Redis.DB = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("negative redis DB accepted")
	}
}
