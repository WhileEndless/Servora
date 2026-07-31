package main

import (
	"flag"
	"fmt"
	"log"
	"os"

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
	server, err := app.NewServer(cfg, version)
	if err != nil {
		log.Fatal(err)
	}
	defer server.Close()
	if err := server.Run(); err != nil {
		log.Printf("monitor stopped: %v", err)
		os.Exit(1)
	}
}
