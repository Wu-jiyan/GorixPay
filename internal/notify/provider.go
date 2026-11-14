package notify

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// Sender delivers one message to one recipient.
//
// The built-in log sender only writes a line to stdout so a developer (or a test)
// can read the code from the terminal. 生产要接真实通道：真实部署里在 app.go 接线
// 时用 `notify.WithSender(...)` 注入一个短信/邮件网关的实现，配置从
// notification_channels 里解密取得；logSender 不能上线使用。
type Sender interface {
	SendSMS(ctx context.Context, to, text string) error
	SendMail(ctx context.Context, to, subject, body string) error
}

// logSender writes messages to an io.Writer (stdout by default). It is the
// development default: nothing leaves the process.
type logSender struct {
	out io.Writer
}

// NewLogSender returns the development sender that prints to out.
func NewLogSender(out io.Writer) Sender {
	if out == nil {
		out = os.Stdout
	}
	return &logSender{out: out}
}

func (sender *logSender) SendSMS(_ context.Context, to, text string) error {
	_, err := fmt.Fprintf(sender.out, "[notify] sms to=%s text=%q\n", to, text)
	return err
}

func (sender *logSender) SendMail(_ context.Context, to, subject, body string) error {
	_, err := fmt.Fprintf(sender.out, "[notify] mail to=%s subject=%q body=%q\n", to, subject, body)
	return err
}

// maskTarget keeps a recipient readable in logs without printing it in full.
func maskTarget(target string) string {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return ""
	}
	if at := strings.IndexByte(trimmed, '@'); at > 0 {
		local := trimmed[:at]
		if len(local) <= 2 {
			return "***" + trimmed[at:]
		}
		return local[:2] + "***" + trimmed[at:]
	}
	if len(trimmed) <= 4 {
		return "***"
	}
	return trimmed[:3] + "****" + trimmed[len(trimmed)-2:]
}
