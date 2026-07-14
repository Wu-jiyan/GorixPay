package secret

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKeyFileSupportsRawAndBase64(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	for name, content := range map[string][]byte{
		"raw":    key,
		"base64": []byte(base64.StdEncoding.EncodeToString(key) + "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "master.key")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadKeyFile(path)
			if err != nil || !bytes.Equal(loaded, key) {
				t.Fatalf("loaded=%x err=%v", loaded, err)
			}
		})
	}
}

func TestLoadKeyFileRejectsInvalidLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyFile(path); err == nil {
		t.Fatal("invalid master key accepted")
	}
}
