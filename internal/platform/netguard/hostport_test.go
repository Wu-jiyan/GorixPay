package netguard

import (
	"net"
	"testing"
)

// TestValidateHostPort covers the SMTP path: the console can type any host, and
// the same rules that guard merchant callback URLs have to apply to a bare
// host:port pair (there is no URL to validate there).
func TestValidateHostPort(t *testing.T) {
	blocked := []struct {
		host string
		port int
	}{
		{"127.0.0.1", 25},
		{"localhost", 587},
		{"smtp.localhost", 465},
		{"10.0.0.5", 25},
		{"192.168.1.10", 587},
		{"169.254.169.254", 80},
		{"::1", 25},
		{"", 25},
		{"smtp.example.com", 0},
		{"smtp.example.com", 70000},
	}
	for _, target := range blocked {
		if err := ValidateHostPort(target.host, target.port); err == nil {
			t.Fatalf("%s:%d 应被拒绝", target.host, target.port)
		}
	}
	allowed := []struct {
		host string
		port int
	}{
		{"smtp.example.com", 587},
		{"smtp.qq.com", 465},
		{"203.0.113.10", 25},
	}
	for _, target := range allowed {
		if err := ValidateHostPort(target.host, target.port); err != nil {
			t.Fatalf("%s:%d 应放行，得到 %v", target.host, target.port, err)
		}
	}
}

// TestControlRejectsLoopback keeps the exported dial hook honest.
func TestControlRejectsLoopback(t *testing.T) {
	hook := Control()
	if hook == nil {
		t.Fatal("Control() 不应为 nil")
	}
	// The hook is exercised through a real dial in netguard_test; here we only
	// assert it exists and refuses a loopback address through the shared rule.
	if !blocked(net.ParseIP("127.0.0.1"), false) {
		t.Fatal("回环地址应被视为拦截目标")
	}
}
