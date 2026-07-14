package session

import "testing"

func TestGenerateAndHashToken(t *testing.T) {
	plain, hash, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if plain == "" || len(hash) != 64 || HashToken(plain) != hash {
		t.Fatal("invalid generated token")
	}
	other, otherHash, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if plain == other || hash == otherHash {
		t.Fatal("tokens are not unique")
	}
}
