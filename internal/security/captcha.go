package security

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

const (
	captchaLength     = 4
	captchaAlphabet   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	captchaWidth      = 128
	captchaHeight     = 44
	captchaNoiseLines = 3
)

var captchaPalette = []string{"#0f172a", "#1d4ed8", "#047857", "#b91c1c", "#7c3aed"}

// Captcha carries the drawn image and, for the service layer only, the answer.
// The answer is never serialized to a client.
type Captcha struct {
	ID   string `json:"id"`
	SVG  string `json:"image_svg"`
	Code string `json:"-"`
}

// NewCaptcha draws four characters into an SVG. Each character gets its own
// rotation and offset and a few noise lines cross the background, so a plain
// text extractor cannot read the code back out.
func NewCaptcha() (Captcha, error) {
	var code strings.Builder
	for index := 0; index < captchaLength; index++ {
		position, err := randomInt(len(captchaAlphabet))
		if err != nil {
			return Captcha{}, err
		}
		code.WriteByte(captchaAlphabet[position])
	}
	svg, err := renderCaptchaSVG(code.String())
	if err != nil {
		return Captcha{}, err
	}
	return Captcha{SVG: svg, Code: code.String()}, nil
}

func renderCaptchaSVG(code string) (string, error) {
	var builder strings.Builder
	fmt.Fprintf(&builder, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="captcha">`,
		captchaWidth, captchaHeight, captchaWidth, captchaHeight)
	builder.WriteString(`<rect width="100%" height="100%" fill="#f8fafc"/>`)
	for index := 0; index < captchaNoiseLines; index++ {
		x1, err := randomInt(captchaWidth)
		if err != nil {
			return "", err
		}
		y1, err := randomInt(captchaHeight)
		if err != nil {
			return "", err
		}
		x2, err := randomInt(captchaWidth)
		if err != nil {
			return "", err
		}
		y2, err := randomInt(captchaHeight)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&builder, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#94a3b8" stroke-width="1" opacity="0.6"/>`, x1, y1, x2, y2)
	}
	for index := 0; index < len(code); index++ {
		offsetX, err := randomInt(5)
		if err != nil {
			return "", err
		}
		offsetY, err := randomInt(9)
		if err != nil {
			return "", err
		}
		rotation, err := randomInt(35)
		if err != nil {
			return "", err
		}
		baseX := 12 + index*28
		baseY := 30 + offsetY - 4
		fmt.Fprintf(&builder, `<text x="%d" y="%d" font-family="monospace" font-size="26" font-weight="700" fill="%s" transform="rotate(%d %d %d)">%c</text>`,
			baseX+offsetX-2, baseY, captchaPalette[index%len(captchaPalette)], rotation-17, baseX, 30, code[index])
	}
	builder.WriteString(`</svg>`)
	return builder.String(), nil
}

func randomInt(limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}
