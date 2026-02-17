package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

const deviceIdentityVersion = 1

type DeviceIdentity struct {
	DeviceID      string
	PublicKeyPem  string
	PrivateKeyPem string
	publicKey     ed25519.PublicKey
	privateKey    ed25519.PrivateKey
}

type deviceIdentityFile struct {
	Version       int    `json:"version"`
	DeviceID      string `json:"deviceId"`
	PublicKeyPem  string `json:"publicKeyPem"`
	PrivateKeyPem string `json:"privateKeyPem"`
	CreatedAtMs   int64  `json:"createdAtMs"`
}

func LoadOrCreateIdentity(path string) (*DeviceIdentity, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		var stored deviceIdentityFile
		if err := json.Unmarshal(data, &stored); err != nil {
			return nil, err
		}
		if stored.PublicKeyPem == "" || stored.PrivateKeyPem == "" {
			return nil, errors.New("gateway: identity missing keys")
		}
		pub, err := parsePublicKeyPem(stored.PublicKeyPem)
		if err != nil {
			return nil, err
		}
		priv, err := parsePrivateKeyPem(stored.PrivateKeyPem)
		if err != nil {
			return nil, err
		}
		derivedID := deviceIDFromPublicKey(pub)
		deviceID := stored.DeviceID
		if deviceID == "" || deviceID != derivedID {
			deviceID = derivedID
			stored.DeviceID = derivedID
			if updated, err := json.MarshalIndent(stored, "", "  "); err == nil {
				if err := os.WriteFile(path, updated, 0o600); err == nil {
					_ = os.Chmod(path, 0o600)
				}
			}
		}
		return &DeviceIdentity{
			DeviceID:      deviceID,
			PublicKeyPem:  stored.PublicKeyPem,
			PrivateKeyPem: stored.PrivateKeyPem,
			publicKey:     pub,
			privateKey:    priv,
		}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	publicPem, err := marshalPublicKeyPem(publicKey)
	if err != nil {
		return nil, err
	}
	privatePem, err := marshalPrivateKeyPem(privateKey)
	if err != nil {
		return nil, err
	}
	deviceID := deviceIDFromPublicKey(publicKey)
	stored := deviceIdentityFile{
		Version:       deviceIdentityVersion,
		DeviceID:      deviceID,
		PublicKeyPem:  publicPem,
		PrivateKeyPem: privatePem,
		CreatedAtMs:   time.Now().UnixMilli(),
	}
	encoded, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return nil, err
	}
	return &DeviceIdentity{
		DeviceID:      deviceID,
		PublicKeyPem:  publicPem,
		PrivateKeyPem: privatePem,
		publicKey:     publicKey,
		privateKey:    privateKey,
	}, nil
}

func (d *DeviceIdentity) PublicKeyRawBase64Url() string {
	if len(d.publicKey) == 0 && d.PublicKeyPem != "" {
		if pub, err := parsePublicKeyPem(d.PublicKeyPem); err == nil {
			d.publicKey = pub
		}
	}
	return base64URLEncode([]byte(d.publicKey))
}

func (d *DeviceIdentity) Sign(payload string) string {
	if len(d.privateKey) == 0 && d.PrivateKeyPem != "" {
		if priv, err := parsePrivateKeyPem(d.PrivateKeyPem); err == nil {
			d.privateKey = priv
		}
	}
	if len(d.privateKey) == 0 {
		return ""
	}
	signature := ed25519.Sign(d.privateKey, []byte(payload))
	return base64URLEncode(signature)
}

func BuildDeviceAuthPayload(deviceID, clientID, clientMode, role string, scopes []string, signedAtMs int64, token, nonce string) string {
	scopeValue := strings.Join(scopes, ",")
	version := "v1"
	if nonce != "" {
		version = "v2"
	}
	parts := []string{
		version,
		deviceID,
		clientID,
		clientMode,
		role,
		scopeValue,
		strconv.FormatInt(signedAtMs, 10),
		token,
	}
	if version == "v2" {
		parts = append(parts, nonce)
	}
	return strings.Join(parts, "|")
}

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func deviceIDFromPublicKey(publicKey ed25519.PublicKey) string {
	hash := sha256.Sum256(publicKey)
	return hex.EncodeToString(hash[:])
}

func parsePublicKeyPem(value string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("gateway: invalid public key pem")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("gateway: unexpected public key type")
	}
	return publicKey, nil
}

func parsePrivateKeyPem(value string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("gateway: invalid private key pem")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("gateway: unexpected private key type")
	}
	return privateKey, nil
}

func marshalPublicKeyPem(publicKey ed25519.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

func marshalPrivateKeyPem(privateKey ed25519.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}
