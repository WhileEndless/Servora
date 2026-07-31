package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/WhileEndless/Servora/internal/agent"
	"github.com/WhileEndless/Servora/internal/app"
)

var version = "0.0.1"

func main() {
	configPath := flag.String("config", "/etc/system-maintenance/monitor.conf", "configuration file")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	cfg, err := app.LoadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	srv := agent.New(cfg.AgentSocket, cfg)
	if err := srv.Run(); err != nil {
		log.Printf("agent stopped: %v", err)
		os.Exit(1)
	}
}
