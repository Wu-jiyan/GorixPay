package config

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"gorixpay/internal/platform/secret"
)

// Config is the encrypted channel configuration. APIURL/PID/Key are the
// historical epay-shaped fields kept for compatibility; Extra carries the
// plugin-specific settings (app_id, private_key, mch_id, ...) so new plugins do
// not need a schema migration. The whole struct is stored encrypted, unchanged.
type Config struct {
	APIURL string            `json:"api_url"`
	PID    string            `json:"pid"`
	Key    string            `json:"key"`
	Extra  map[string]string `json:"extra,omitempty"`
}

// Values flattens the configuration into the map[string]string shape plugins
// consume. Extra wins over a same-named legacy key so a channel can override
// api_url/pid/key from the generic config form.
func (config Config) Values() map[string]string {
	values := make(map[string]string, len(config.Extra)+3)
	if config.APIURL != "" {
		values["api_url"] = config.APIURL
	}
	if config.PID != "" {
		values["pid"] = config.PID
	}
	if config.Key != "" {
		values["key"] = config.Key
	}
	for key, value := range config.Extra {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		values[key] = value
	}
	return values
}

type Service struct {
	cipher *secret.Cipher
}

func NewService(cipher *secret.Cipher) *Service {
	return &Service{cipher: cipher}
}

func (service *Service) Encrypt(channelID string, config Config) (string, error) {
	config.Extra = normalizeExtra(config.Extra)
	if err := validate(channelID, config); err != nil {
		return "", err
	}
	plain, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return service.cipher.Encrypt(plain, []byte("payment-channel:"+channelID))
}

func (service *Service) Decrypt(channelID, encrypted string) (Config, error) {
	plain, _, err := service.cipher.Decrypt(encrypted, []byte("payment-channel:"+channelID))
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(plain, &config); err != nil {
		return Config{}, errors.New("invalid channel config")
	}
	config.Extra = normalizeExtra(config.Extra)
	if err := validate(channelID, config); err != nil {
		return Config{}, err
	}
	return config, nil
}

// normalizeExtra trims the extra map and drops blank keys/values so an empty
// form field never reaches a plugin as "present but useless".
func normalizeExtra(extra map[string]string) map[string]string {
	if len(extra) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(extra))
	for key, value := range extra {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		normalized[trimmedKey] = trimmedValue
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// validate keeps the historical epay requirement (api_url + pid + key, all or
// none) but lets plugins that only live off Extra (alipay, wechatpayv3, ...)
// store their configuration without the legacy trio.
func validate(channelID string, config Config) error {
	if strings.TrimSpace(channelID) == "" {
		return errors.New("invalid channel config")
	}
	apiURL := strings.TrimSpace(config.APIURL)
	pid := strings.TrimSpace(config.PID)
	key := strings.TrimSpace(config.Key)
	legacyAny := apiURL != "" || pid != "" || key != ""
	legacyAll := apiURL != "" && pid != "" && key != ""
	switch {
	case legacyAny && !legacyAll:
		// A half-filled legacy triple is always a mistake, never a new-style config.
		return errors.New("invalid channel config")
	case !legacyAny && len(config.Extra) == 0:
		return errors.New("invalid channel config")
	}
	if apiURL != "" {
		parsed, err := url.Parse(apiURL)
		if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return errors.New("invalid channel api url")
		}
	}
	return nil
}
