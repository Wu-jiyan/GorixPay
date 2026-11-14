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
	captchaNoiseLines = 4
	captchaCell       = 4
)

var captchaPalette = []string{"#0f172a", "#1d4ed8", "#047857", "#b91c1c", "#7c3aed"}

// captchaGlyphs is a 5x7 bitmap font for captchaAlphabet. The image is drawn
// from these pixels instead of <text> elements on purpose: an SVG that spells
// the answer in a text node can be read back by a script, which makes the whole
// captcha pointless.
var captchaGlyphs = map[byte][7]string{
	'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"},
	'B': {"11110", "10001", "10001", "11110", "10001", "10001", "11110"},
	'C': {"01111", "10000", "10000", "10000", "10000", "10000", "01111"},
	'D': {"11110", "10001", "10001", "10001", "10001", "10001", "11110"},
	'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
	'F': {"11111", "10000", "10000", "11110", "10000", "10000", "10000"},
	'G': {"01111", "10000", "10000", "10011", "10001", "10001", "01110"},
	'H': {"10001", "10001", "10001", "11111", "10001", "10001", "10001"},
	'J': {"00111", "00010", "00010", "00010", "00010", "10010", "01100"},
	'K': {"10001", "10010", "10100", "11000", "10100", "10010", "10001"},
	'L': {"10000", "10000", "10000", "10000", "10000", "10000", "11111"},
	'M': {"10001", "11011", "10101", "10101", "10001", "10001", "10001"},
	'N': {"10001", "11001", "10101", "10011", "10001", "10001", "10001"},
	'P': {"11110", "10001", "10001", "11110", "10000", "10000", "10000"},
	'Q': {"01110", "10001", "10001", "10001", "10101", "10010", "01101"},
	'R': {"11110", "10001", "10001", "11110", "10100", "10010", "10001"},
	'S': {"01111", "10000", "10000", "01110", "00001", "00001", "11110"},
	'T': {"11111", "00100", "00100", "00100", "00100", "00100", "00100"},
	'U': {"10001", "10001", "10001", "10001", "10001", "10001", "01110"},
	'V': {"10001", "10001", "10001", "10001", "10001", "01010", "00100"},
	'W': {"10001", "10001", "10001", "10101", "10101", "11011", "10001"},
	'X': {"10001", "10001", "01010", "00100", "01010", "10001", "10001"},
	'Y': {"10001", "10001", "01010", "00100", "00100", "00100", "00100"},
	'Z': {"11111", "00001", "00010", "00100", "01000", "10000", "11111"},
	'2': {"01110", "10001", "00001", "00110", "01000", "10000", "11111"},
	'3': {"11110", "00001", "00001", "01110", "00001", "00001", "11110"},
	'4': {"00010", "00110", "01010", "10010", "11111", "00010", "00010"},
	'5': {"11111", "10000", "11110", "00001", "00001", "10001", "01110"},
	'6': {"00110", "01000", "10000", "11110", "10001", "10001", "01110"},
	'7': {"11111", "00001", "00010", "00100", "01000", "01000", "01000"},
	'8': {"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
	'9': {"01110", "10001", "10001", "01111", "00001", "00010", "01100"},
}

// Captcha carries the drawn image and, for the service layer only, the answer.
// The answer is never serialized to a client.
type Captcha struct {
	ID   string `json:"id"`
	SVG  string `json:"image_svg"`
	Code string `json:"-"`
}

// NewCaptcha draws four characters into an SVG. Each glyph is painted as a set
// of jittered pixels from a bitmap font, so the code cannot be recovered by
// reading text nodes out of the markup.
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
		fmt.Fprintf(&builder, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#94a3b8" stroke-width="1" opacity="0.55"/>`, x1, y1, x2, y2)
	}
	for index := 0; index < len(code); index++ {
		glyph, ok := captchaGlyphs[code[index]]
		if !ok {
			return "", fmt.Errorf("captcha: no glyph for %q", code[index])
		}
		offsetX, err := randomInt(6)
		if err != nil {
			return "", err
		}
		offsetY, err := randomInt(8)
		if err != nil {
			return "", err
		}
		rotation, err := randomInt(31)
		if err != nil {
			return "", err
		}
		originX := 10 + index*29 + offsetX
		originY := 6 + offsetY
		colour := captchaPalette[index%len(captchaPalette)]
		fmt.Fprintf(&builder, `<g fill="%s" transform="rotate(%d %d %d)">`, colour, rotation-15, originX+12, originY+14)
		for row := 0; row < 7; row++ {
			for column := 0; column < 5; column++ {
				if glyph[row][column] != '1' {
					continue
				}
				jitterX, err := randomInt(3)
				if err != nil {
					return "", err
				}
				jitterY, err := randomInt(3)
				if err != nil {
					return "", err
				}
				x := originX + column*captchaCell + jitterX - 1
				y := originY + row*captchaCell + jitterY - 1
				fmt.Fprintf(&builder, `<rect x="%d" y="%d" width="%d" height="%d"/>`, x, y, captchaCell, captchaCell)
			}
		}
		builder.WriteString(`</g>`)
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
