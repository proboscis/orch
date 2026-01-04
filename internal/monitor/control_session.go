package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const controlSessionFile = "control-session.json"

type ControlSession struct {
	SessionID string `json:"session_id"`
	Port      int    `json:"port"`
}

func LoadControlSession(orchDir string) *ControlSession {
	if orchDir == "" {
		return nil
	}

	path := filepath.Join(orchDir, controlSessionFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var session ControlSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil
	}

	if session.SessionID == "" {
		return nil
	}

	return &session
}

func SaveControlSession(orchDir string, session *ControlSession) error {
	if orchDir == "" {
		return nil
	}
	if session == nil || session.SessionID == "" {
		return nil
	}

	if err := os.MkdirAll(orchDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(orchDir, controlSessionFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}
