package qqpay

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"sort"
	"strings"
)

// canonical renders the sorted "k=v&k2=v2" string QQ Pay signs. Empty values and
// the sign key itself are skipped, matching the WeChat-style rule QQ inherits.
func canonical(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for name, value := range values {
		if name == "sign" || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, name := range keys {
		parts = append(parts, name+"="+values[name])
	}
	return strings.Join(parts, "&")
}

// signParams produces the upper-case MD5 signature: md5(stringA + "&key=" + key).
func signParams(values map[string]string, key string) string {
	sum := md5.Sum([]byte(canonical(values) + "&key=" + key))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// verifySignature compares the payload signature in constant time (both sides are
// normalized to upper case first).
func verifySignature(values map[string]string, key string) bool {
	signature := strings.ToUpper(strings.TrimSpace(values["sign"]))
	if signature == "" {
		return false
	}
	expected := signParams(values, key)
	if len(expected) != len(signature) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

// nonceString returns the 32 character nonce_str QQ Pay expects.
func nonceString() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// encodeXML renders the flat <xml>...</xml> document QQ Pay accepts.
func encodeXML(values map[string]string) (string, error) {
	keys := make([]string, 0, len(values))
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteString("<xml>")
	for _, name := range keys {
		builder.WriteString("<")
		builder.WriteString(name)
		builder.WriteString(">")
		var escaped bytes.Buffer
		if err := xml.EscapeText(&escaped, []byte(values[name])); err != nil {
			return "", err
		}
		builder.Write(escaped.Bytes())
		builder.WriteString("</")
		builder.WriteString(name)
		builder.WriteString(">")
	}
	builder.WriteString("</xml>")
	return builder.String(), nil
}

// decodeXML flattens a <xml> document into a key/value map. CDATA sections and
// plain text nodes are treated identically, which is what both QQ Pay and WeChat
// Pay emit.
func decodeXML(raw []byte) (map[string]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	fields := make(map[string]string)
	current := ""
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local == "xml" {
				continue
			}
			current = typed.Name.Local
		case xml.CharData:
			if current == "" {
				continue
			}
			fields[current] += string(typed)
		case xml.EndElement:
			current = ""
		}
	}
	if len(fields) == 0 {
		return nil, errors.New("empty xml document")
	}
	return fields, nil
}
