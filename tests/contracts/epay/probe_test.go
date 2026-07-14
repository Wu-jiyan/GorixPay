package epay

import (
	"encoding/json"
	"os/exec"
	"testing"
)

type probeResult struct {
	Canonical string `json:"canonical"`
	MD5       string `json:"md5"`
	RSA       string `json:"rsa"`
	Verified  bool   `json:"verified"`
}

func TestPHPProbeMatchesGoReference(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php CLI unavailable: " + err.Error())
	}
	output, err := exec.Command(php, "probe/signing.php").CombinedOutput()
	if err != nil {
		t.Fatalf("PHP probe failed: %v: %s", err, output)
	}
	var result probeResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode PHP probe: %v: %s", err, output)
	}
	vector := loadRSAVector(t)
	if result.Canonical != vector.SignContent {
		t.Fatalf("PHP canonical = %q, want %q", result.Canonical, vector.SignContent)
	}
	if result.MD5 != MD5Sign(vector.Params, "test-key") {
		t.Fatalf("PHP MD5 = %q, want Go reference", result.MD5)
	}
	if result.RSA != vector.Signature || !result.Verified {
		t.Fatal("PHP RSA result does not match fixed Go vector")
	}
}
