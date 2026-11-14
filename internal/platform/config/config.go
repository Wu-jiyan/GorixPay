package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// defaultNotifyTimeoutSeconds matches netguard.DefaultTimeout: long enough for a
// slow merchant endpoint, short enough that one dead endpoint cannot stall the
// notification queue for minutes.
const (
	defaultNotifyTimeoutSeconds = 15
	maxNotifyTimeoutSeconds     = 300
)

type Config struct {
	Environment string         `yaml:"environment"`
	HTTP        HTTPConfig     `yaml:"http"`
	Database    DatabaseConfig `yaml:"database"`
	Security    SecurityConfig `yaml:"security"`
	Redis       RedisConfig    `yaml:"redis"`
	Sandbox     SandboxConfig  `yaml:"sandbox"`
}

type HTTPConfig struct {
	Address   string `yaml:"address"`
	PublicURL string `yaml:"public_url"`
	// TrustedProxies lists the proxy addresses whose X-Forwarded-For header is
	// believed. Empty means "trust nobody", so ClientIP reports the socket peer:
	// otherwise any client can spoof its address and walk past the IP blacklist,
	// the login lockout and the cashier rate limit.
	TrustedProxies []string `yaml:"trusted_proxies"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

type SecurityConfig struct {
	MasterKeyFile string `yaml:"master_key_file"`
	// NotifyTimeoutSeconds bounds one merchant notification round trip. A
	// merchant server that accepts the connection and never answers must not be
	// able to park the outbox worker, so this is always enforced.
	NotifyTimeoutSeconds int `yaml:"notify_timeout_seconds"`
	// AllowPrivateNotifyHosts permits merchant notify_url targets on private
	// (RFC 1918 / ULA) networks. Loopback, link-local and cloud metadata
	// addresses are rejected regardless.
	AllowPrivateNotifyHosts bool `yaml:"allow_private_notify_hosts"`
}

// RedisConfig describes the optional Redis dependency. An empty Addr disables
// Redis entirely and every dependent feature degrades to its local fallback.
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	Prefix   string `yaml:"prefix"`
}

// SandboxConfig enables the local sandbox payment channel. It defaults to true
// in development so the full payment loop can be exercised locally; Validate
// forces it off in production regardless of configuration input.
type SandboxConfig struct {
	Enabled bool `yaml:"enabled"`
}

func Load(args []string, getenv func(string) string) (Config, error) {
	cfg := Config{
		Environment: "development",
		HTTP:        HTTPConfig{Address: ":8080", PublicURL: "http://localhost:8080"},
		Database:    DatabaseConfig{Driver: "sqlite", DSN: "gorixpay.db"},
		Security:    SecurityConfig{NotifyTimeoutSeconds: defaultNotifyTimeoutSeconds},
		Sandbox:     SandboxConfig{Enabled: true},
	}
	configPath := flagValue(args, "config")
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return Config{}, err
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, err
		}
	}
	applyEnvironment(&cfg, getenv)
	flags := flag.NewFlagSet("gorixpay", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.String("config", configPath, "configuration file")
	flags.StringVar(&cfg.Environment, "environment", cfg.Environment, "runtime environment")
	flags.StringVar(&cfg.HTTP.Address, "http-address", cfg.HTTP.Address, "HTTP listen address")
	flags.StringVar(&cfg.HTTP.PublicURL, "public-url", cfg.HTTP.PublicURL, "public base URL")
	flags.StringVar(&cfg.Database.Driver, "database-driver", cfg.Database.Driver, "database driver")
	flags.StringVar(&cfg.Database.DSN, "database-dsn", cfg.Database.DSN, "database DSN")
	flags.StringVar(&cfg.Security.MasterKeyFile, "master-key-file", cfg.Security.MasterKeyFile, "master key file")
	flags.IntVar(&cfg.Security.NotifyTimeoutSeconds, "notify-timeout-seconds", cfg.Security.NotifyTimeoutSeconds, "merchant notification timeout in seconds")
	flags.BoolVar(&cfg.Security.AllowPrivateNotifyHosts, "allow-private-notify-hosts", cfg.Security.AllowPrivateNotifyHosts, "allow merchant notify_url on private networks")
	flags.StringVar(&cfg.Redis.Addr, "redis-addr", cfg.Redis.Addr, "Redis address (empty disables Redis)")
	flags.BoolVar(&cfg.Sandbox.Enabled, "sandbox-enabled", cfg.Sandbox.Enabled, "enable the sandbox payment channel")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	switch c.Database.Driver {
	case "sqlite", "mysql", "postgres":
	default:
		return fmt.Errorf("unsupported database driver %q", c.Database.Driver)
	}
	if c.HTTP.Address == "" || c.Database.DSN == "" {
		return errors.New("HTTP address and database DSN are required")
	}
	publicURL, err := url.ParseRequestURI(c.HTTP.PublicURL)
	if err != nil || publicURL.Host == "" || (publicURL.Scheme != "http" && publicURL.Scheme != "https") {
		return errors.New("public URL must be an absolute HTTP URL")
	}
	if c.Environment == "production" && c.Security.MasterKeyFile == "" {
		return errors.New("production requires a master key file")
	}
	if c.Environment == "production" {
		c.Sandbox.Enabled = false
	}
	if c.Security.NotifyTimeoutSeconds <= 0 {
		c.Security.NotifyTimeoutSeconds = defaultNotifyTimeoutSeconds
	}
	if c.Security.NotifyTimeoutSeconds > maxNotifyTimeoutSeconds {
		c.Security.NotifyTimeoutSeconds = maxNotifyTimeoutSeconds
	}
	if c.Redis.DB < 0 {
		return errors.New("redis DB index must not be negative")
	}
	return nil
}

func applyEnvironment(cfg *Config, getenv func(string) string) {
	applyString(&cfg.Environment, getenv("GORIXPAY_ENVIRONMENT"))
	applyString(&cfg.HTTP.Address, getenv("GORIXPAY_HTTP_ADDRESS"))
	applyString(&cfg.HTTP.PublicURL, getenv("GORIXPAY_PUBLIC_URL"))
	applyList(&cfg.HTTP.TrustedProxies, getenv("GORIXPAY_TRUSTED_PROXIES"))
	applyString(&cfg.Database.Driver, getenv("GORIXPAY_DATABASE_DRIVER"))
	applyString(&cfg.Database.DSN, getenv("GORIXPAY_DATABASE_DSN"))
	applyString(&cfg.Security.MasterKeyFile, getenv("GORIXPAY_MASTER_KEY_FILE"))
	applyInt(&cfg.Security.NotifyTimeoutSeconds, getenv("GORIXPAY_NOTIFY_TIMEOUT_SECONDS"))
	applyBool(&cfg.Security.AllowPrivateNotifyHosts, getenv("GORIXPAY_ALLOW_PRIVATE_NOTIFY_HOSTS"))
	applyString(&cfg.Redis.Addr, getenv("GORIXPAY_REDIS_ADDR"))
	applyString(&cfg.Redis.Password, getenv("GORIXPAY_REDIS_PASSWORD"))
	applyInt(&cfg.Redis.DB, getenv("GORIXPAY_REDIS_DB"))
	applyString(&cfg.Redis.Prefix, getenv("GORIXPAY_REDIS_PREFIX"))
	applyBool(&cfg.Sandbox.Enabled, getenv("GORIXPAY_SANDBOX_ENABLED"))
}

func applyString(target *string, value string) {
	if value != "" {
		*target = value
	}
}

func applyBool(target *bool, value string) {
	if value == "" {
		return
	}
	if parsed, err := strconv.ParseBool(value); err == nil {
		*target = parsed
	}
}

// applyList splits a comma separated environment value, trimming blanks so a
// trailing comma does not become an empty proxy entry.
func applyList(target *[]string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	items := make([]string, 0, 4)
	for _, item := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	if len(items) > 0 {
		*target = items
	}
}

func applyInt(target *int, value string) {
	if value == "" {
		return
	}
	if parsed, err := strconv.Atoi(value); err == nil {
		*target = parsed
	}
}

func flagValue(args []string, name string) string {
	long := "--" + name
	for index, value := range args {
		if value == long && index+1 < len(args) {
			return args[index+1]
		}
		if len(value) > len(long) && value[:len(long)+1] == long+"=" {
			return value[len(long)+1:]
		}
	}
	return ""
}
