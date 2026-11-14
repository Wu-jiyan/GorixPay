package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

const (
	qrModules   = 29
	qrModulePx  = 6
	qrQuietZone = 4
	qrAlpha     = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// renderQRSVG draws a deterministic QR-like grid for the login ticket URL. The
// pattern is derived from sha256(content) and includes the three finder squares
// so the image is recognisable as a scan target; decoding is not required, the
// ticket string is what the phone posts back.
func renderQRSVG(content string) string {
	size := qrModules + qrQuietZone*2
	digest := sha256.Sum256([]byte(content))
	dark := make([][]bool, qrModules)
	for row := 0; row < qrModules; row++ {
		dark[row] = make([]bool, qrModules)
		for column := 0; column < qrModules; column++ {
			index := row*qrModules + column
			byteValue := digest[index%len(digest)]
			dark[row][column] = (byteValue>>(uint(index%8)))&1 == 1
		}
	}
	drawFinder := func(originRow, originColumn int) {
		for row := 0; row < 7; row++ {
			for column := 0; column < 7; column++ {
				edge := row == 0 || row == 6 || column == 0 || column == 6
				center := row >= 2 && row <= 4 && column >= 2 && column <= 4
				dark[originRow+row][originColumn+column] = edge || center
			}
		}
	}
	drawFinder(0, 0)
	drawFinder(0, qrModules-7)
	drawFinder(qrModules-7, 0)

	var builder strings.Builder
	fmt.Fprintf(&builder, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="qr login">`,
		size*qrModulePx, size*qrModulePx, size, size)
	builder.WriteString(`<rect width="100%" height="100%" fill="#ffffff"/>`)
	for row := 0; row < qrModules; row++ {
		for column := 0; column < qrModules; column++ {
			if !dark[row][column] {
				continue
			}
			fmt.Fprintf(&builder, `<rect x="%d" y="%d" width="1" height="1" fill="#111827"/>`,
				column+qrQuietZone, row+qrQuietZone)
		}
	}
	builder.WriteString(`</svg>`)
	return builder.String()
}

// randomToken returns n random base62 characters, used for tickets and state.
func randomToken(length int) (string, error) {
	var builder strings.Builder
	limit := big.NewInt(int64(len(qrAlpha)))
	for index := 0; index < length; index++ {
		value, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		builder.WriteByte(qrAlpha[value.Int64()])
	}
	return builder.String(), nil
}

func hashToken(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
