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

	"gopkg.in/yaml.v3"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Config holds global server settings
type Config struct {
	MCPServer struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"mcp_server"`
	GraphQL struct {
		URL string `yaml:"url"`
	} `yaml:"graphql"`
	Auth struct {
		TokenScript string `yaml:"token_script"`
	} `yaml:"auth"`
}

// ToolConfig holds one tool’s YAML definition
type ToolConfig struct {
	Tool struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	} `yaml:"tool"`
	GraphQL struct {
		Query string `yaml:"query"`
	} `yaml:"graphql"`
	Inputs []struct {
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
	req.Header.Set("Authorization", "Bearer "+token)

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
func makeHandler(cfg Config, tcfg ToolConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// 1. Gather variables
		vars := map[string]interface{}{}
		for _, inp := range tcfg.Inputs {
			val, ok := req.Params.Arguments[inp.Name]
			if !ok && inp.Required {
				return nil, fmt.Errorf("missing required argument: %s", inp.Name)
			}
			vars[inp.Name] = val
		}

		// 2. Run token script
		// Use sh -c to execute the token script string as a shell command
		cmd := exec.Command("sh", "-c", cfg.Auth.TokenScript)
		out, err := cmd.Output()
		if err != nil {
			// Include stderr in the error message if available
			if exitErr, ok := err.(*exec.ExitError); ok {
				return nil, fmt.Errorf("auth script failed: %w, stderr: %s", err, string(exitErr.Stderr))
			}
			return nil, fmt.Errorf("auth script failed: %w", err)
		}
		token := string(bytes.TrimSpace(out))

		// 3. Call GraphQL
		result, err := executeGraphQL(cfg.GraphQL.URL, tcfg.GraphQL.Query, vars, token)
		if err != nil {
			return nil, err
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
	var cfg Config
	forgeConfigPath := filepath.Join(configDir, "forge.yaml")
	if err := loadConfig(forgeConfigPath, &cfg); err != nil {
		panic(fmt.Errorf("unable to load %s: %w", forgeConfigPath, err))
	}

	// Initialize MCP server
	srv := server.NewMCPServer(cfg.MCPServer.Name, cfg.MCPServer.Version)

	// Discover tool config files in the config directory
	toolPattern := filepath.Join(configDir, "*.yaml")
	files, err := filepath.Glob(toolPattern)
	if err != nil {
		panic(fmt.Errorf("reading tools from %s: %w", configDir, err))
	}

	// Register each tool
	for _, file := range files {
		// Skip the main forge.yaml file
		if filepath.Base(file) == "forge.yaml" {
			continue
		}

		var tcfg ToolConfig
		if err := loadConfig(file, &tcfg); err != nil {
			panic(fmt.Errorf("parsing %s: %w", file, err))
		}

		// Build a slice of ToolOption: description + one WithX per input
		opts := []mcp.ToolOption{
			mcp.WithDescription(tcfg.Tool.Description),
		}

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
				panic(fmt.Errorf("unsupported input type %q in %s", inp.Type, file))
			}
		}

		// Create and register the tool with all options at once
		tool := mcp.NewTool(tcfg.Tool.Name, opts...)
		srv.AddTool(tool, makeHandler(cfg, tcfg))
	}

	// Start serving on stdio
	if err := server.ServeStdio(srv); err != nil {
		panic(fmt.Errorf("MCP server terminated: %w", err))
	}
}
