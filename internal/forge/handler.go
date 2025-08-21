package forge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// CtxAuthKey is used as a key for storing auth tokens in context
type CtxAuthKey struct{}

// CreateMCPServer creates and configures an MCP server with all tools registered
func CreateMCPServer(appConfig *AppConfig, version string) (*server.MCPServer, error) {
	// Init MCP server
	srv := server.NewMCPServer(appConfig.Config.Name, version)

	// Discover & register tools
	if err := RegisterTools(srv, appConfig.Config, appConfig.ConfigDir, appConfig.IsDebug); err != nil {
		return nil, fmt.Errorf("registering tools: %w", err)
	}

	return srv, nil
}

// RegisterTools discovers and registers all tools from the config directory
func RegisterTools(srv *server.MCPServer, cfg *ForgeConfig, configDir string, isDebug bool) error {
	// Discover & register tools
	files, err := filepath.Glob(filepath.Join(configDir, "*.yaml"))
	if err != nil {
		return fmt.Errorf("error discovering tools: %w", err)
	}

	for _, f := range files {
		if filepath.Base(f) == "forge.yaml" {
			continue
		}

		tcfg, err := LoadToolConfig(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s: %v\n", f, err)
			continue
		}

		opts := []mcp.ToolOption{
			mcp.WithDescription(tcfg.Description),
		}

		// Add annotations if specified
		if tcfg.Annotations.Title != "" {
			opts = append(opts, mcp.WithTitleAnnotation(tcfg.Annotations.Title))
		}
		if tcfg.Annotations.ReadOnlyHint != nil {
			opts = append(opts, mcp.WithReadOnlyHintAnnotation(*tcfg.Annotations.ReadOnlyHint))
		}
		if tcfg.Annotations.DestructiveHint != nil {
			opts = append(opts, mcp.WithDestructiveHintAnnotation(*tcfg.Annotations.DestructiveHint))
		}
		if tcfg.Annotations.IdempotentHint != nil {
			opts = append(opts, mcp.WithIdempotentHintAnnotation(*tcfg.Annotations.IdempotentHint))
		}
		if tcfg.Annotations.OpenWorldHint != nil {
			opts = append(opts, mcp.WithOpenWorldHintAnnotation(*tcfg.Annotations.OpenWorldHint))
		}

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
		srv.AddTool(tool, makeHandler(*cfg, *tcfg, isDebug))
	}

	return nil
}

// makeHandler produces a ToolHandler for the given configs
func makeHandler(cfg ForgeConfig, tcfg ToolConfig, isDebug bool) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// 1. Gather variables
		vars := map[string]interface{}{}
		args := req.GetArguments()
		for _, inp := range tcfg.Inputs {
			val, ok := args[inp.Name]
			if !ok && inp.Required {
				return mcp.NewToolResultError(fmt.Sprintf("missing required argument: %s", inp.Name)), nil
			}
			vars[inp.Name] = val
		}

		// 2. Get the token
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

			// Build merged environment: start with os.Environ() if passthrough, else start empty,
			// then overlay values from cfg.Env to ensure overrides.
			var envList []string
			if cfg.EnvPassthrough {
				envList = os.Environ()
			} else {
				envList = []string{}
			}

			for key, value := range cfg.Env {
				// Remove any existing entries for this key
				prefix := key + "="
				filtered := envList[:0]
				for _, e := range envList {
					if !strings.HasPrefix(e, prefix) {
						filtered = append(filtered, e)
					}
				}
				envList = append(filtered, fmt.Sprintf("%s=%s", key, value))
			}

			cmd.Env = envList

			if isDebug {
				log.Printf("Executing token command: %s", cfg.TokenCommand)
				if len(cmd.Env) > 0 {
					log.Printf("Environment variables: %v", cmd.Env)
				}
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
			token = "Bearer " + string(bytes.TrimSpace(out))

			if isDebug {
				log.Printf("Obtained token (sha256): %x\n", sha256.Sum256([]byte(token)))
			}
		} else {
			// No token command specified, proceed with pass through token
			token, _ = ctx.Value(CtxAuthKey{}).(string)

			if isDebug {
				log.Printf("Pass through token (sha256): %x\n", sha256.Sum256([]byte(token)))
			}
		}

		// 3. Call GraphQL
		res, err := ExecuteGraphQL(cfg.URL, tcfg.Query, vars, token, isDebug)
		if err != nil {
			// Return error result to MCP instead of terminating
			return mcp.NewToolResultErrorFromErr("GraphQL execution failed", err), nil
		}

		// 4. Return raw JSON
		return mcp.NewToolResultText(string(res)), nil
	}
}
