package page

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

// The scan page needs a real QR symbol. rsc.io/qr encodes the payload and this
// file paints the modules as SVG rectangles; the same approach as the merchant
// pay code, kept local so the package has no dependency on merchanttools.
const (
	qrModulePx  = 6
	qrQuietZone = 4
)

// renderQRSVG draws a scannable symbol for the channel URL. An unencodable
// payload falls back to an empty SVG instead of painting something that merely
// looks like a code.
func renderQRSVG(content string) string {
	code, err := qr.Encode(content, qr.M)
	if err != nil {
		return `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1" viewBox="0 0 1 1" role="img" aria-label="支付二维码"></svg>`
	}
	size := code.Size + qrQuietZone*2
	var builder strings.Builder
	fmt.Fprintf(&builder, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="支付二维码" shape-rendering="crispEdges">`,
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
