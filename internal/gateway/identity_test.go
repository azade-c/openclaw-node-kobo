package gateway

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
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

func TestLoadOrCreateIdentity_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupted file: %v", err)
	}
	if _, err := LoadOrCreateIdentity(path); err == nil {
		t.Fatalf("expected error for corrupted device.json")
	}
}

func TestLoadOrCreateIdentity_MissingKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")
	stored := deviceIdentityFile{
		Version:       deviceIdentityVersion,
		DeviceID:      "device-id",
		PublicKeyPem:  "",
		PrivateKeyPem: "",
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal stored: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write device file: %v", err)
	}
	if _, err := LoadOrCreateIdentity(path); err == nil {
		t.Fatalf("expected error for missing keys")
	}
}

func TestLoadOrCreateIdentity_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")
	if _, err := LoadOrCreateIdentity(path); err != nil {
		t.Fatalf("create identity: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat device.json: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %v", info.Mode().Perm())
	}
}

func TestLoadOrCreateIdentity_DerivedDeviceID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	stored := deviceIdentityFile{
		Version:       deviceIdentityVersion,
		DeviceID:      "",
		PublicKeyPem:  identity.PublicKeyPem,
		PrivateKeyPem: identity.PrivateKeyPem,
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal stored: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write device file: %v", err)
	}
	reloaded, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	publicKey, err := parsePublicKey(reloaded.PublicKeyPem)
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	expected := deviceIDFromPublicKey(publicKey)
	if reloaded.DeviceID != expected {
		t.Fatalf("expected derived device id")
	}
}

func TestDeviceIdentity_SignEmptyPayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	signature := identity.Sign("")
	if signature == "" {
		t.Fatalf("expected signature for empty payload")
	}
}

func TestDeviceIdentity_SignConsistency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	payload := "same-payload"
	first := identity.Sign(payload)
	second := identity.Sign(payload)
	if first != second {
		t.Fatalf("expected deterministic signatures")
	}
}

func TestDeviceIdentity_PublicKeyRawBase64Url_Deterministic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	first := identity.PublicKeyRawBase64Url()
	second := identity.PublicKeyRawBase64Url()
	if first != second {
		t.Fatalf("expected deterministic public key encoding")
	}
}

func TestBuildDeviceAuthPayload_EmptyScopes(t *testing.T) {
	payloadNil := BuildDeviceAuthPayload(
		"device",
		"client",
		"mode",
		"role",
		nil,
		1,
		"token",
		"",
	)
	payloadEmpty := BuildDeviceAuthPayload(
		"device",
		"client",
		"mode",
		"role",
		[]string{},
		1,
		"token",
		"",
	)
	if payloadNil != payloadEmpty {
		t.Fatalf("expected nil and empty scopes to match")
	}
}

func TestBuildDeviceAuthPayload_EmptyToken(t *testing.T) {
	payload := BuildDeviceAuthPayload(
		"device",
		"client",
		"mode",
		"role",
		[]string{"scope"},
		42,
		"",
		"",
	)
	expected := "v1|device|client|mode|role|scope|42|"
	if payload != expected {
		t.Fatalf("unexpected payload: %s", payload)
	}
}

func TestBuildDeviceAuthPayload_SpecialCharacters(t *testing.T) {
	payload := BuildDeviceAuthPayload(
		"dev|ice",
		"cli|ent",
		"mo|de",
		"ro|le",
		[]string{"scope|a", "scope|b"},
		100,
		"to|ken",
		"non|ce",
	)
	expected := "v2|dev|ice|cli|ent|mo|de|ro|le|scope|a,scope|b|100|to|ken|non|ce"
	if payload != expected {
		t.Fatalf("unexpected payload: %s", payload)
	}
}

func TestBuildDeviceAuthPayload_V1VsV2(t *testing.T) {
	t.Run("v1 when nonce empty", func(t *testing.T) {
		payload := BuildDeviceAuthPayload(
			"device",
			"client",
			"mode",
			"role",
			[]string{"scope"},
			10,
			"token",
			"",
		)
		if !strings.HasPrefix(payload, "v1|") {
			t.Fatalf("expected v1 payload, got %s", payload)
		}
	})
	t.Run("v2 when nonce set", func(t *testing.T) {
		payload := BuildDeviceAuthPayload(
			"device",
			"client",
			"mode",
			"role",
			[]string{"scope"},
			10,
			"token",
			"nonce",
		)
		if !strings.HasPrefix(payload, "v2|") {
			t.Fatalf("expected v2 payload, got %s", payload)
		}
	})
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
