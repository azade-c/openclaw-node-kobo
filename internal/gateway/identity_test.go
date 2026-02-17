package gateway

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")

	first, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	second, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	if first.DeviceID != second.DeviceID {
		t.Fatalf("device id mismatch")
	}
	if first.PublicKeyPem != second.PublicKeyPem {
		t.Fatalf("public key mismatch")
	}
	if first.PrivateKeyPem != second.PrivateKeyPem {
		t.Fatalf("private key mismatch")
	}
}

func TestIdentitySignVerify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	payload := BuildDeviceAuthPayload(
		"device",
		"client",
		"mode",
		"role",
		nil,
		123,
		"token",
		"nonce",
	)
	signature := identity.Sign(payload)
	sigBytes, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	publicKey, err := parsePublicKey(identity.PublicKeyPem)
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	if !ed25519.Verify(publicKey, []byte(payload), sigBytes) {
		t.Fatalf("signature did not verify")
	}
}

func TestBuildDeviceAuthPayloadFormat(t *testing.T) {
	payloadV2 := BuildDeviceAuthPayload(
		"device-id",
		"client-id",
		"client-mode",
		"node",
		[]string{"scope-a", "scope-b"},
		1700000000000,
		"token-value",
		"nonce-value",
	)
	expectedV2 := "v2|device-id|client-id|client-mode|node|scope-a,scope-b|1700000000000|token-value|nonce-value"
	if payloadV2 != expectedV2 {
		t.Fatalf("unexpected payload: %s", payloadV2)
	}
	payloadV1 := BuildDeviceAuthPayload(
		"device-id",
		"client-id",
		"client-mode",
		"node",
		[]string{"scope-a", "scope-b"},
		1700000000000,
		"token-value",
		"",
	)
	expectedV1 := "v1|device-id|client-id|client-mode|node|scope-a,scope-b|1700000000000|token-value"
	if payloadV1 != expectedV1 {
		t.Fatalf("unexpected payload: %s", payloadV1)
	}
}

func TestBase64UrlEncoding(t *testing.T) {
	input := []byte{0xfb, 0xef, 0xff}
	standard := base64.StdEncoding.EncodeToString(input)
	expected := strings.TrimRight(strings.ReplaceAll(strings.ReplaceAll(standard, "+", "-"), "/", "_"), "=")
	encoded := base64URLEncode(input)
	if encoded != expected {
		t.Fatalf("unexpected base64url: %s", encoded)
	}
	if strings.Contains(encoded, "+") || strings.Contains(encoded, "/") || strings.Contains(encoded, "=") {
		t.Fatalf("base64url contains invalid characters: %s", encoded)
	}
}

func TestPublicKeyRawBase64UrlLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	encoded := identity.PublicKeyRawBase64Url()
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	if len(decoded) != ed25519.PublicKeySize {
		t.Fatalf("expected %d bytes, got %d", ed25519.PublicKeySize, len(decoded))
	}
}

func TestDeviceIDDerivation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	publicKey, err := parsePublicKey(identity.PublicKeyPem)
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	hash := sha256.Sum256(publicKey)
	expected := hex.EncodeToString(hash[:])
	if identity.DeviceID != expected {
		t.Fatalf("unexpected device id")
	}
}

func parsePublicKey(pemValue string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemValue))
	if block == nil {
		return nil, errInvalidPEM
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errUnexpectedKeyType
	}
	return publicKey, nil
}

var errInvalidPEM = errors.New("invalid pem")
var errUnexpectedKeyType = errors.New("unexpected key type")
