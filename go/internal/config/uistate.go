package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type UIState struct {
	ExpandedNodes []string `json:"expanded_nodes"`
	SelectedNode  string   `json:"selected_node"`
	FocusedSide   bool     `json:"focused_side"`
	ActiveView    string   `json:"active_view"`
}

func GetUIStatePath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "ui_state.json"), nil
}

func SaveUIState(state UIState) error {
	path, err := GetUIStatePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadUIState() (UIState, error) {
	path, err := GetUIStatePath()
	if err != nil {
		return UIState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return UIState{}, err
	}
	var state UIState
	if err := json.Unmarshal(data, &state); err != nil {
		return UIState{}, err
	}
	return state, nil
}
