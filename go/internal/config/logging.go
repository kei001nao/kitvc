package config

import (
	"log"
	"os"
	"path/filepath"
)

func InitLogging(configDir string) {
	logPath := filepath.Join(configDir, "kitvc.log")
	f, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return
	}
	log.SetOutput(f)
}
