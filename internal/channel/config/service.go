package config

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"gorixpay/internal/platform/secret"
)

type Config struct {
	APIURL string `json:"api_url"`
	PID    string `json:"pid"`
	Key    string `json:"key"`
}

type Service struct {
	cipher *secret.Cipher
}

func NewService(cipher *secret.Cipher) *Service {
	return &Service{cipher: cipher}
}

func (service *Service) Encrypt(channelID string, config Config) (string, error) {
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
	if err := validate(channelID, config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validate(channelID string, config Config) error {
	if strings.TrimSpace(channelID) == "" || strings.TrimSpace(config.PID) == "" || strings.TrimSpace(config.Key) == "" {
		return errors.New("invalid channel config")
	}
	parsed, err := url.Parse(config.APIURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("invalid channel api url")
	}
	return nil
}
