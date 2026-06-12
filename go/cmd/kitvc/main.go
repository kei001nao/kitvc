package main

import (
	"fmt"
	"os"

	"kitvc/internal/config"
	"kitvc/internal/db"
	"kitvc/internal/ui"

	tea "charm.land/bubbletea/v2"
)

func main() {
	cfgDir, err := config.GetConfigDir()
	if err != nil {
		fmt.Printf("Error getting config directory: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		fmt.Printf("Error creating config directory: %v\n", err)
		os.Exit(1)
	}

	config.InitLogging(cfgDir)

	if err := db.InitDB(cfgDir); err != nil {
		fmt.Printf("Error initializing database: %v\n", err)
		os.Exit(1)
	}
	defer db.CloseDB()

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(ui.InitialModel(cfg))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
