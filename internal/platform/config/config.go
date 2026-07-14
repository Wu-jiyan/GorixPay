package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Environment string         `yaml:"environment"`
	HTTP        HTTPConfig     `yaml:"http"`
	Database    DatabaseConfig `yaml:"database"`
	Security    SecurityConfig `yaml:"security"`
}

type HTTPConfig struct {
	Address   string `yaml:"address"`
	PublicURL string `yaml:"public_url"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

type SecurityConfig struct {
	MasterKeyFile string `yaml:"master_key_file"`
}

func Load(args []string, getenv func(string) string) (Config, error) {
	cfg := Config{
		Environment: "development",
		HTTP:        HTTPConfig{Address: ":8080", PublicURL: "http://localhost:8080"},
		Database:    DatabaseConfig{Driver: "sqlite", DSN: "gorixpay.db"},
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
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
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
	return nil
}

func applyEnvironment(cfg *Config, getenv func(string) string) {
	applyString(&cfg.Environment, getenv("GORIXPAY_ENVIRONMENT"))
	applyString(&cfg.HTTP.Address, getenv("GORIXPAY_HTTP_ADDRESS"))
	applyString(&cfg.HTTP.PublicURL, getenv("GORIXPAY_PUBLIC_URL"))
	applyString(&cfg.Database.Driver, getenv("GORIXPAY_DATABASE_DRIVER"))
	applyString(&cfg.Database.DSN, getenv("GORIXPAY_DATABASE_DSN"))
	applyString(&cfg.Security.MasterKeyFile, getenv("GORIXPAY_MASTER_KEY_FILE"))
}

func applyString(target *string, value string) {
	if value != "" {
		*target = value
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
