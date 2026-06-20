package main

import (
	"fmt"
	"log"
	"os"

	"kitvc/internal/config"
	"kitvc/internal/db"
	"kitvc/internal/ui"

	tea "charm.land/bubbletea/v2"
)

func main() {
	log.Printf("[DEBUG] main: starting")

	cfgDir, err := config.GetConfigDir()
	if err != nil {
		fmt.Printf("Error getting config directory: %v\n", err)
		os.Exit(1)
	}
	log.Printf("[DEBUG] main: cfgDir=%s", cfgDir)

	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		fmt.Printf("Error creating config directory: %v\n", err)
		os.Exit(1)
	}

	config.InitLogging(cfgDir)
	log.Printf("[DEBUG] main: logging initialized")

	if err := db.InitDB(cfgDir); err != nil {
		fmt.Printf("Error initializing database: %v\n", err)
		os.Exit(1)
	}
	defer db.CloseDB()
	log.Printf("[DEBUG] main: db initialized")

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	log.Printf("[DEBUG] main: config loaded")

	m := ui.InitialModel(cfg)
	log.Printf("[DEBUG] main: model created, starting bubbletea")

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}

	log.Printf("[DEBUG] main: bubbletea exited")
}
