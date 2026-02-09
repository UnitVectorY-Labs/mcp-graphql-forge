package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"

	"github.com/UnitVectorY-Labs/mcp-graphql-forge/internal/forge"
)

var Version = "dev" // This will be set by the build systems to the release version

func main() {
	// Set the build version from the build info if not set by the build system
	if Version == "dev" || Version == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
				Version = bi.Main.Version
			}
		}
	}

	// CLI flags
	var httpAddr string
	var forgeConfigFlag string
	var forgeDebugFlag bool

	flag.StringVar(&httpAddr, "http", "", "run HTTP streamable transport on the given address, e.g. 8080 (defaults to stdio if empty)")
	flag.StringVar(&forgeConfigFlag, "forgeConfig", "", "path to the folder containing forge.yaml and tool definitions (overrides FORGE_CONFIG env)")
	flag.BoolVar(&forgeDebugFlag, "forgeDebug", false, "enable debug logging (overrides FORGE_DEBUG env)")

	flag.Parse()

	// Load and validate configuration
	appConfig, err := forge.LoadAppConfig(forgeConfigFlag, forgeDebugFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Setup logging based on debug mode
	if appConfig.IsDebug {
		log.SetOutput(os.Stderr)
		log.Println("Debug mode enabled.")
	} else {
		log.SetOutput(io.Discard)
	}

	// Create and configure MCP server
	srv, err := forge.CreateMCPServer(appConfig, Version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating MCP server: %v\n", err)
		os.Exit(1)
	}

	// Start the server
	serveOpts := forge.ServeOptions{
		HTTPAddr: httpAddr,
		IsDebug:  appConfig.IsDebug,
	}

	if err := forge.Serve(srv, serveOpts); err != nil {
		log.Fatalf("Fatal: %v\n", err)
	}
}
