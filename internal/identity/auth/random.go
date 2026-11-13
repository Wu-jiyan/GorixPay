package auth

import (
	"crypto/rand"
	"math/big"
)

func randomLegacyUID() (int64, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(90000000))
	if err != nil {
		return 0, err
	}
	return value.Int64() + 10000000, nil
}
