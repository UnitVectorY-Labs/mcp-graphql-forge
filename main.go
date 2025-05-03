package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

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

var isDebug bool

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

	if isDebug {
		log.Println("--- GraphQL Request ---")
		if dump, err := httputil.DumpRequestOut(req, true); err == nil {
			log.Printf("%s\n", dump)
		} else {
			log.Printf("dump error: %v\n", err)
		}
		log.Println("-----------------------")
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

	if isDebug {
		log.Println("--- GraphQL Response ---")
		log.Printf("Status Code: %d\n", resp.StatusCode)
		// Attempt to pretty-print JSON response body if possible
		var pretty bytes.Buffer
		if json.Indent(&pretty, respBody, "", "  ") == nil {
			log.Printf("Body:\n%s\n", pretty.String())
		} else {
			// Fallback to printing raw body if not valid JSON
			log.Printf("Body (raw): %s\n", respBody)
		}
		log.Println("------------------------")
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
					stderr := string(bytes.TrimSpace(exitErr.Stderr))
					if stderr != "" {
						errMsg = fmt.Sprintf("%s: %v Stderr: %s", errMsg, exitErr, stderr)
					} else {
						errMsg = fmt.Sprintf("%s: %v", errMsg, exitErr)
					}
				}
				// Return nil error for MCP result error
				return mcp.NewToolResultErrorFromErr(errMsg, err), nil
			}
			token = string(bytes.TrimSpace(out))
			if isDebug {
				log.Printf("Obtained token: %s\n", token)
			}
		}

		// 3. Call GraphQL
		res, err := executeGraphQL(cfg.URL, tcfg.Query, vars, token)
		if err != nil {
			// Return error result to MCP instead of terminating
			return mcp.NewToolResultErrorFromErr("GraphQL execution failed", err), nil
		}

		// 4. Return raw JSON
		return mcp.NewToolResultText(string(res)), nil
	}
}

func main() {
	// CLI flag for SSM/HTTP mode
	var sseAddr string
	flag.StringVar(&sseAddr, "sse", "", "run in SSM (HTTP/SSE) mode on the given address, e.g. :8080")
	flag.Parse()

	// Config dir
	configDir := os.Getenv("FORGE_CONFIG")
	if configDir == "" {
		configDir = "."
	}

	// Debug mode
	isDebug, _ = strconv.ParseBool(os.Getenv("FORGE_DEBUG"))
	if isDebug {
		log.SetOutput(os.Stderr)
		log.Println("Debug mode enabled.")
	} else {
		log.SetOutput(io.Discard)
	}

	// Load core forge.yaml
	var cfg ForgeConfig
	if err := loadConfig(filepath.Join(configDir, "forge.yaml"), &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading core config: %v\n", err)
		os.Exit(1)
	}

	// Init MCP server
	srv := server.NewMCPServer(cfg.Name, cfg.Version)

	// Discover & register tools
	files, err := filepath.Glob(filepath.Join(configDir, "*.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error discovering tools: %v\n", err)
		os.Exit(1)
	}
	for _, f := range files {
		if filepath.Base(f) == "forge.yaml" {
			continue
		}
		var tcfg ToolConfig
		if err := loadConfig(f, &tcfg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s: %v\n", f, err)
			continue
		}

		opts := []mcp.ToolOption{mcp.WithDescription(tcfg.Description)}
		valid := true
		for _, inp := range tcfg.Inputs {
			pOpts := []mcp.PropertyOption{mcp.Description(inp.Description)}
			if inp.Required {
				pOpts = append(pOpts, mcp.Required())
			}
			switch inp.Type {
			case "string":
				opts = append(opts, mcp.WithString(inp.Name, pOpts...))
			case "number":
				opts = append(opts, mcp.WithNumber(inp.Name, pOpts...))
			default:
				fmt.Fprintf(os.Stderr, "Warning: unsupported type %q in %s\n", inp.Type, tcfg.Name)
				valid = false
			}
		}
		if !valid {
			continue
		}
		tool := mcp.NewTool(tcfg.Name, opts...)
		srv.AddTool(tool, makeHandler(cfg, tcfg))
	}

	// Choose mode
	if ssmAddr != "" {
		// SSE mode
		fmt.Printf("Starting MCP server in SSM mode on %s\n", ssmAddr)
		sseSrv := server.NewSSEServer(
			srv,
			server.WithBasePath("/"),
			server.WithSSEEndpoint("/mcp/sse"),
			server.WithMessageEndpoint("/mcp/message"),
		)
		mux := http.NewServeMux()
		mux.Handle("/", sseSrv)

		fmt.Printf("SSE Endpoint: %s\n", sseSrv.CompleteSsePath())
		fmt.Printf("Message Endpoint: %s\n", sseSrv.CompleteMessagePath())

		httpSrv := &http.Server{
			Addr:    ssmAddr,
			Handler: mux,
		}
		if err := httpSrv.ListenAndServe(); err != nil {
			log.Fatalf("SSM server error: %v\n", err)
		}
	} else {
		// stdio mode
		if err := server.ServeStdio(srv); err != nil {
			log.Fatalf("Fatal: MCP server terminated: %v\n", err)
		}
	}
}
