package merchanttools

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

// The pay code is a real QR symbol: rsc.io/qr encodes the payload (the cashier
// URL) and this file paints the modules as SVG rectangles. An earlier draft drew
// a deterministic pattern with finder squares, which looked like a QR code but
// could not be scanned; a code nobody can scan is worse than no code at all.
const (
	qrModulePx  = 6
	qrQuietZone = 4
)

// renderPayCodeSVG draws a scannable QR symbol for the pay code URL.
func renderPayCodeSVG(content string) string {
	code, err := qr.Encode(content, qr.M)
	if err != nil {
		// Too long a payload cannot be encoded; fall back to a plain square so the
		// response stays valid SVG, and let the API surface the URL to the caller.
		return `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1" viewBox="0 0 1 1" role="img" aria-label="merchant pay code"></svg>`
	}
	size := code.Size + qrQuietZone*2
	var builder strings.Builder
	fmt.Fprintf(&builder, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="merchant pay code" shape-rendering="crispEdges">`,
		size*qrModulePx, size*qrModulePx, size, size)
	builder.WriteString(`<rect width="100%" height="100%" fill="#ffffff"/>`)
	for row := 0; row < code.Size; row++ {
		for column := 0; column < code.Size; column++ {
			if !code.Black(column, row) {
				continue
			}
			fmt.Fprintf(&builder, `<rect x="%d" y="%d" width="1" height="1" fill="#111827"/>`,
				column+qrQuietZone, row+qrQuietZone)
		}
	}
	builder.WriteString(`</svg>`)
	return builder.String()
}
