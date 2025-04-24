package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ForgeConfig holds global server settings
type ForgeConfig struct {
	Name         string `yaml:"name"`
	Version      string `yaml:"version"`
	URL          string `yaml:"url"`
	TokenCommand string `yaml:"token_command"`
}

// ToolConfig holds one tool's YAML definition
type ToolConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Query       string `yaml:"query"`
	Inputs      []struct {
		Name        string `yaml:"name"`
		Type        string `yaml:"type"` // "string" or "number"
		Description string `yaml:"description"`
		Required    bool   `yaml:"required"`
	} `yaml:"inputs"`
}

// graphqlRequest is the POST payload for GraphQL
type graphqlRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// loadConfig reads YAML from path into out
func loadConfig(path string, out interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}

// executeGraphQL posts a query+vars to url with Bearer token, returning raw JSON
func executeGraphQL(url, query string, vars map[string]interface{}, token string) ([]byte, error) {
	payload := graphqlRequest{Query: query, Variables: vars}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal GraphQL payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return respBody, nil
}

// makeHandler produces a ToolHandler for the given configs
func makeHandler(cfg ForgeConfig, tcfg ToolConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// 1. Gather variables
		vars := map[string]interface{}{}
		for _, inp := range tcfg.Inputs {
			val, ok := req.Params.Arguments[inp.Name]
			if !ok && inp.Required {
				return mcp.NewToolResultError(fmt.Sprintf("missing required argument: %s", inp.Name)), nil
			}
			vars[inp.Name] = val
		}

		// 2. Run token script
		token := ""
		if cfg.TokenCommand != "" {
			var cmd *exec.Cmd
			// Use the appropriate shell based on the OS
			if runtime.GOOS == "windows" {
				cmd = exec.Command("cmd", "/C", cfg.TokenCommand)
			} else {
				// Assume Unix-like shell for macOS, Linux, etc.
				cmd = exec.Command("sh", "-c", cfg.TokenCommand)
			}

			// Only get a token if the command is specified
			out, err := cmd.Output()
			if err != nil {
				// Include stderr in the error message if available
				errMsg := "token_command failed"
				if exitErr, ok := err.(*exec.ExitError); ok {
					// Combine exit error message and stderr for better context
					stderrMsg := string(bytes.TrimSpace(exitErr.Stderr))
					if stderrMsg != "" {
						errMsg = fmt.Sprintf("%s: %s Stderr: %s", errMsg, exitErr, stderrMsg) // Corrected format string
					} else {
						errMsg = fmt.Sprintf("%s: %s", errMsg, exitErr)
					}
					// Return original error too, but nil for MCP result error
					return mcp.NewToolResultErrorFromErr(errMsg, err), nil
				}
				// Return nil error for MCP result error
				return mcp.NewToolResultErrorFromErr(errMsg, err), nil
			}
			token = string(bytes.TrimSpace(out))
		}

		// 3. Call GraphQL
		result, err := executeGraphQL(cfg.URL, tcfg.Query, vars, token)
		if err != nil {
			// Return error result to MCP instead of terminating
			return mcp.NewToolResultErrorFromErr("GraphQL execution failed", err), nil
		}

		// 4. Return raw JSON
		return mcp.NewToolResultText(string(result)), nil
	}
}

func main() {
	// Get config directory from environment variable, default to "."
	configDir := os.Getenv("FORGE_CONFIG")
	if configDir == "" {
		configDir = "." // Default to current directory if not set
	}

	// Load forge.yaml
	var cfg ForgeConfig
	forgeConfigPath := filepath.Join(configDir, "forge.yaml")
	if err := loadConfig(forgeConfigPath, &cfg); err != nil {
		// Log error and exit gracefully if main config fails
		fmt.Fprintf(os.Stderr, "Error: unable to load core configuration %s: %v\n", forgeConfigPath, err)
		os.Exit(1)
	}

	// Initialize MCP server
	srv := server.NewMCPServer(cfg.Name, cfg.Version)

	// Discover tool config files in the config directory
	toolPattern := filepath.Join(configDir, "*.yaml")
	files, err := filepath.Glob(toolPattern)
	if err != nil {
		// Log error and exit gracefully if tool discovery fails
		fmt.Fprintf(os.Stderr, "Error: failed reading tool configurations from %s: %v\n", configDir, err)
		os.Exit(1)
	}

	// Register each tool
	for _, file := range files {
		// Skip the main forge.yaml file
		if filepath.Base(file) == "forge.yaml" {
			continue
		}

		var tcfg ToolConfig
		if err := loadConfig(file, &tcfg); err != nil {
			// Log error for specific tool config and continue
			fmt.Fprintf(os.Stderr, "Warning: skipping tool - failed parsing %s: %v\n", file, err)
			continue
		}

		// Build a slice of ToolOption: description + one WithX per input
		opts := []mcp.ToolOption{
			mcp.WithDescription(tcfg.Description),
		}

		validTool := true // Flag to track if the tool definition is valid
		for _, inp := range tcfg.Inputs {
			// Collect property options per input
			propOpts := []mcp.PropertyOption{
				mcp.Description(inp.Description),
			}
			if inp.Required {
				propOpts = append(propOpts, mcp.Required())
			}

			// Choose the right WithX and append to opts
			switch inp.Type {
			case "string":
				opts = append(opts, mcp.WithString(inp.Name, propOpts...))
			case "number":
				opts = append(opts, mcp.WithNumber(inp.Name, propOpts...))
			default:
				// Log error for unsupported type and mark tool as invalid
				fmt.Fprintf(os.Stderr, "Warning: skipping tool %q - unsupported input type %q in %s\n", tcfg.Name, inp.Type, file)
				validTool = false
				break // Exit the inner loop for this tool
			}
		}

		// Only register the tool if its definition was valid
		if validTool {
			tool := mcp.NewTool(tcfg.Name, opts...)
			srv.AddTool(tool, makeHandler(cfg, tcfg))
		}
	}

	// Start serving on stdio
	if err := server.ServeStdio(srv); err != nil {
		// Log fatal error if server fails to start/run
		fmt.Fprintf(os.Stderr, "Fatal: MCP server terminated: %v\n", err)
		os.Exit(1) // Exit with error status
	}
}
