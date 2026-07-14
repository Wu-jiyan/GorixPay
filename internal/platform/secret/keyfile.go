package secret

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
)

func LoadKeyFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 32 {
		return data, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("master key must contain 32 raw bytes or their base64 encoding")
	}
	return decoded, nil
}
