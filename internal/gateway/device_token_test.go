package gateway

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadDeviceToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(path, "token-value"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	token, err := LoadDeviceToken(path)
	if err != nil {
		t.Fatalf("load token: %v", err)
	}
	if token != "token-value" {
		t.Fatalf("expected token-value, got %q", token)
	}
}

func TestLoadDeviceToken_NotExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")
	token, err := LoadDeviceToken(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Fatalf("expected empty token")
	}
}

func TestLoadDeviceToken_CorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device-token.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatalf("write corrupted json: %v", err)
	}
	if _, err := LoadDeviceToken(path); err == nil {
		t.Fatalf("expected error for corrupted json")
	}
}

func TestSaveDeviceToken_Permissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(path, "token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %v", info.Mode().Perm())
	}
}

func TestSaveDeviceToken_Overwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(path, "first"); err != nil {
		t.Fatalf("save first token: %v", err)
	}
	if err := SaveDeviceToken(path, "second"); err != nil {
		t.Fatalf("save second token: %v", err)
	}
	token, err := LoadDeviceToken(path)
	if err != nil {
		t.Fatalf("load token: %v", err)
	}
	if token != "second" {
		t.Fatalf("expected second token, got %q", token)
	}
}

func TestClearDeviceToken_Exists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(path, "token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	if err := ClearDeviceToken(path); err != nil {
		t.Fatalf("clear token: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected token file removed")
	}
}

func TestClearDeviceToken_NotExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")
	if err := ClearDeviceToken(path); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestClearDeviceToken_EmptyPath(t *testing.T) {
	if err := ClearDeviceToken(""); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDeviceTokenRoundtrip_WithTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(path, "token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	var stored deviceTokenFile
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("unmarshal token file: %v", err)
	}
	if stored.SavedAtMs == 0 {
		t.Fatalf("expected savedAtMs to be populated")
	}
}
