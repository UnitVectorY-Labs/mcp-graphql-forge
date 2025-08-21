package forge

import (
	"context"
	"fmt"
	"net/http"

	"github.com/mark3labs/mcp-go/server"
)

// ServeOptions holds options for serving the MCP server
type ServeOptions struct {
	HTTPAddr string
	IsDebug  bool
}

// Serve starts the MCP server in either HTTP or stdio mode
func Serve(srv *server.MCPServer, opts ServeOptions) error {
	if opts.HTTPAddr != "" {
		return serveHTTP(srv, opts.HTTPAddr, opts.IsDebug)
	}
	return serveStdio(srv)
}

// serveHTTP starts the server in HTTP mode
func serveHTTP(srv *server.MCPServer, httpAddr string, isDebug bool) error {
	if isDebug {
		fmt.Printf("Starting MCP server using Streamable HTTP transport on %s\n", httpAddr)
	}

	streamSrv := server.NewStreamableHTTPServer(
		srv,
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			// Inject authorization token into context
			if auth := r.Header.Get("Authorization"); auth != "" {
				ctx = context.WithValue(ctx, CtxAuthKey{}, auth)
			}
			return ctx
		}),
	)

	if isDebug {
		fmt.Printf("Streamable HTTP Endpoint: http://localhost:%s/mcp\n", httpAddr)
	}

	if err := streamSrv.Start(":" + httpAddr); err != nil {
		return fmt.Errorf("streamable HTTP server error: %w", err)
	}

	return nil
}

// serveStdio starts the server in stdio mode
func serveStdio(srv *server.MCPServer) error {
	if err := server.ServeStdio(srv); err != nil {
		return fmt.Errorf("MCP server terminated: %w", err)
	}
	return nil
}
