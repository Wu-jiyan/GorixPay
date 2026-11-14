package invitecode

import (
	"crypto/rand"
	"math/big"
	"strings"
)

// randomCode returns an 8 character code from an alphabet without look-alikes.
func randomCode() (string, error) {
	limit := big.NewInt(int64(len(codeAlphabet)))
	var builder strings.Builder
	for index := 0; index < codeLength; index++ {
		value, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		builder.WriteByte(codeAlphabet[value.Int64()])
	}
	return builder.String(), nil
}
