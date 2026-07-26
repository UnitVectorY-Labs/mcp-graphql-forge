package forge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/toon-format/toon-go"
)

// CtxAuthKey is used as a key for storing auth tokens in context
type CtxAuthKey struct{}

// CreateMCPServer creates and configures an MCP server with all tools registered
func CreateMCPServer(appConfig *AppConfig, version string) (*mcp.Server, error) {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    appConfig.Config.Name,
		Version: version,
	}, nil)

	if err := RegisterTools(srv, appConfig.Config, appConfig.ConfigDir, appConfig.IsDebug); err != nil {
		return nil, fmt.Errorf("registering tools: %w", err)
	}

	return srv, nil
}

// RegisterTools discovers and registers all tools from the config directory
func RegisterTools(srv *mcp.Server, cfg *ForgeConfig, configDir string, isDebug bool) error {
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

		valid := true
		properties := make(map[string]*jsonschema.Schema)
		var required []string

		for _, inp := range tcfg.Inputs {
			var propSchema *jsonschema.Schema
			switch inp.Type {
			case "string":
				propSchema = &jsonschema.Schema{
					Type:        "string",
					Description: inp.Description,
				}
			case "number":
				propSchema = &jsonschema.Schema{
					Type:        "number",
					Description: inp.Description,
				}
			default:
				fmt.Fprintf(os.Stderr, "Warning: unsupported type %q in %s\n", inp.Type, tcfg.Name)
				valid = false
				break
			}
			if propSchema != nil {
				properties[inp.Name] = propSchema
				if inp.Required {
					required = append(required, inp.Name)
				}
			}
		}
		if !valid {
			continue
		}

		inputSchema := &jsonschema.Schema{
			Type:       "object",
			Properties: properties,
			Required:   required,
		}

		var annotations *mcp.ToolAnnotations
		if tcfg.Annotations.Title != "" ||
			tcfg.Annotations.ReadOnlyHint != nil ||
			tcfg.Annotations.DestructiveHint != nil ||
			tcfg.Annotations.IdempotentHint != nil ||
			tcfg.Annotations.OpenWorldHint != nil {
			annotations = &mcp.ToolAnnotations{
				Title: tcfg.Annotations.Title,
			}
			if tcfg.Annotations.ReadOnlyHint != nil {
				annotations.ReadOnlyHint = *tcfg.Annotations.ReadOnlyHint
			}
			if tcfg.Annotations.DestructiveHint != nil {
				annotations.DestructiveHint = tcfg.Annotations.DestructiveHint
			}
			if tcfg.Annotations.IdempotentHint != nil {
				annotations.IdempotentHint = *tcfg.Annotations.IdempotentHint
			}
			if tcfg.Annotations.OpenWorldHint != nil {
				annotations.OpenWorldHint = tcfg.Annotations.OpenWorldHint
			}
		}

		tool := &mcp.Tool{
			Name:        tcfg.Name,
			Description: tcfg.Description,
			InputSchema: inputSchema,
			Annotations: annotations,
		}

		tc := *tcfg
		handler := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to parse arguments: %v", err)}},
						IsError: true,
					}, nil
				}
			}

			vars := map[string]any{}
			for _, inp := range tc.Inputs {
				val, ok := args[inp.Name]
				if !ok && inp.Required {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("missing required argument: %s", inp.Name)}},
						IsError: true,
					}, nil
				}
				vars[inp.Name] = val
			}

			token := ""
			if cfg.TokenCommand != "" {
				var cmd *exec.Cmd
				if runtime.GOOS == "windows" {
					cmd = exec.Command("cmd", "/C", cfg.TokenCommand)
				} else {
					cmd = exec.Command("sh", "-c", cfg.TokenCommand)
				}

				var envList []string
				if cfg.EnvPassthrough {
					envList = os.Environ()
				} else {
					envList = []string{}
				}

				for key, value := range cfg.Env {
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

				out, err := cmd.Output()
				if err != nil {
					errMsg := "token_command failed"
					if exitErr, ok := err.(*exec.ExitError); ok {
						stderr := string(bytes.TrimSpace(exitErr.Stderr))
						if stderr != "" {
							errMsg = fmt.Sprintf("%s: %v Stderr: %s", errMsg, exitErr, stderr)
						} else {
							errMsg = fmt.Sprintf("%s: %v", errMsg, exitErr)
						}
					}
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: errMsg}},
						IsError: true,
					}, nil
				}
				token = "Bearer " + string(bytes.TrimSpace(out))

				if isDebug {
					log.Printf("Obtained token (sha256): %x\n", sha256.Sum256([]byte(token)))
				}
			} else {
				token, _ = ctx.Value(CtxAuthKey{}).(string)

				if isDebug {
					log.Printf("Pass through token (sha256): %x\n", sha256.Sum256([]byte(token)))
				}
			}

			res, err := ExecuteGraphQL(cfg.URL, tc.Query, vars, token, isDebug)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("GraphQL execution failed: %v", err)}},
					IsError: true,
				}, nil
			}

			result := processOutput(res, tc.Output, isDebug)

			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: result}},
			}, nil
		}

		srv.AddTool(tool, handler)
	}

	return nil
}

// processOutput converts the GraphQL response based on the output format
func processOutput(res []byte, output string, isDebug bool) string {
	if output == "" {
		output = "raw"
	}

	switch output {
	case "raw":
		return string(res)
	case "json":
		return processJSONOutput(res, isDebug, func(jsonData any) ([]byte, error) {
			return json.Marshal(jsonData)
		}, "minimization")
	case "toon":
		return processJSONOutput(res, isDebug, func(jsonData any) ([]byte, error) {
			return toon.Marshal(jsonData)
		}, "TOON conversion")
	default:
		if isDebug {
			log.Printf("Warning: unknown output type %q, defaulting to raw", output)
		}
		return string(res)
	}
}

// processJSONOutput is a helper that unmarshals JSON and applies a transformation function
func processJSONOutput(res []byte, isDebug bool, transformFunc func(any) ([]byte, error), operationName string) string {
	var jsonData any
	if err := json.Unmarshal(res, &jsonData); err != nil {
		if isDebug {
			log.Printf("Warning: failed to parse JSON for %s, returning raw: %v", operationName, err)
		}
		return string(res)
	}

	transformed, err := transformFunc(jsonData)
	if err != nil {
		if isDebug {
			log.Printf("Warning: failed to perform %s, returning raw: %v", operationName, err)
		}
		return string(res)
	}

	return string(transformed)
}
