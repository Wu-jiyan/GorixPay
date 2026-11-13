package wechatpayv3

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// apiV3KeyLength is the fixed AES-256-GCM key size used by WeChat Pay v3.
const apiV3KeyLength = 32

var errInvalidCiphertext = errors.New("invalid wechat pay ciphertext")

// signMessage produces the base64 RSA-SHA256 (PKCS#1 v1.5) signature of an
// APIv3 message, the value carried in the Authorization header or paySign.
func signMessage(privateKey *rsa.PrivateKey, message string) (string, error) {
	if privateKey == nil {
		return "", errors.New("missing wechat pay private key")
	}
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

// verifyMessage checks an upstream signature against the platform public key.
func verifyMessage(publicKey *rsa.PublicKey, message, signature string) bool {
	if publicKey == nil || signature == "" {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	digest := sha256.Sum256([]byte(message))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], decoded) == nil
}

// authorization renders the WECHATPAY2-SHA256-RSA2048 header value.
func authorization(mchID, serialNo, nonce, timestamp, signature string) string {
	return fmt.Sprintf(
		`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`,
		mchID, nonce, signature, timestamp, serialNo,
	)
}

// requestMessage is the canonical signed string of an APIv3 request.
func requestMessage(method, path, timestamp, nonce, body string) string {
	return method + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + body + "\n"
}

// notificationMessage is the canonical signed string of an APIv3 notification.
func notificationMessage(timestamp, nonce, body string) string {
	return timestamp + "\n" + nonce + "\n" + body + "\n"
}

// nonceString returns a 32 character nonce suitable for the Authorization
// header and JSAPI pay parameters.
func nonceString() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
