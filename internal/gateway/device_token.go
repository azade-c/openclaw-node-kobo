package gateway

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

type deviceTokenFile struct {
	Token     string `json:"token"`
	SavedAtMs int64  `json:"savedAtMs"`
}

func LoadDeviceToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var stored deviceTokenFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return "", err
	}
	return stored.Token, nil
}

func SaveDeviceToken(path string, token string) error {
	stored := deviceTokenFile{
		Token:     token,
		SavedAtMs: time.Now().UnixMilli(),
	}
	encoded, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o600)
}
