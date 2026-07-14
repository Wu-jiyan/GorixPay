package epay

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type rsaVector struct {
	ID            string         `yaml:"id"`
	PrivateKey    string         `yaml:"private_key"`
	PublicKey     string         `yaml:"public_key"`
	Params        map[string]any `yaml:"params"`
	SignContent   string         `yaml:"sign_content"`
	Signature     string         `yaml:"signature"`
	ProductionUse string         `yaml:"production_use"`
}

func loadRSAVector(t *testing.T) rsaVector {
	t.Helper()
	data, err := os.ReadFile("rsa_vectors.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var vector rsaVector
	if err := yaml.Unmarshal(data, &vector); err != nil {
		t.Fatal(err)
	}
	return vector
}

func TestRSAVectorUsesSHA256PKCS1v15(t *testing.T) {
	vector := loadRSAVector(t)
	if vector.ProductionUse != "forbidden" {
		t.Fatal("RSA vector must forbid production use")
	}
	if got := CanonicalContent(vector.Params); got != vector.SignContent {
		t.Fatalf("sign content = %q, want %q", got, vector.SignContent)
	}
	privatePEM, _ := os.ReadFile(vector.PrivateKey)
	signature, err := RSASign(vector.Params, privatePEM)
	if err != nil {
		t.Fatal(err)
	}
	if signature != vector.Signature {
		t.Fatalf("signature = %q, want fixed vector", signature)
	}
	publicPEM, _ := os.ReadFile(vector.PublicKey)
	if err := RSAVerify(vector.Params, signature, publicPEM); err != nil {
		t.Fatal(err)
	}
}

func TestRSARejectsTamperingWrongKeyAndPSS(t *testing.T) {
	vector := loadRSAVector(t)
	publicPEM, _ := os.ReadFile(vector.PublicKey)
	tampered := cloneParams(vector.Params)
	tampered["amount"] = "2.00"
	if err := RSAVerify(tampered, vector.Signature, publicPEM); err == nil {
		t.Fatal("tampered parameters verified")
	}
	wrongKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	wrongDER, _ := x509.MarshalPKIXPublicKey(&wrongKey.PublicKey)
	wrongPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: wrongDER})
	if err := RSAVerify(vector.Params, vector.Signature, wrongPEM); err == nil {
		t.Fatal("wrong public key verified")
	}
	privatePEM, _ := os.ReadFile(vector.PrivateKey)
	block, _ := pem.Decode(privatePEM)
	privateKey, _ := x509.ParsePKCS8PrivateKey(block.Bytes)
	digest := sha256.Sum256([]byte(vector.SignContent))
	pss, _ := rsa.SignPSS(rand.Reader, privateKey.(*rsa.PrivateKey), crypto.SHA256, digest[:], nil)
	if err := RSAVerify(vector.Params, base64.StdEncoding.EncodeToString(pss), publicPEM); err == nil {
		t.Fatal("RSA-PSS signature verified as PKCS1 v1.5")
	}
}

func TestRSALowercaseSignTypeFallsBackToMD5(t *testing.T) {
	params := map[string]any{"pid": 1001, "sign_type": "rsa"}
	if got, want := SignByType(params, "key", nil), MD5Sign(params, "key"); got != want {
		t.Fatalf("lowercase rsa signature = %q, want MD5 %q", got, want)
	}
}

func cloneParams(params map[string]any) map[string]any {
	result := make(map[string]any, len(params))
	for key, value := range params {
		result[key] = value
	}
	return result
}
