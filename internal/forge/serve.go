package forge

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServeOptions holds options for serving the MCP server
type ServeOptions struct {
	HTTPAddr string
	IsDebug  bool
}

// Serve starts the MCP server in either HTTP or stdio mode
func Serve(srv *mcp.Server, opts ServeOptions) error {
	if opts.HTTPAddr != "" {
		return serveHTTP(srv, opts.HTTPAddr, opts.IsDebug)
	}
	return serveStdio(srv)
}

// serveHTTP starts the server in HTTP mode
func serveHTTP(srv *mcp.Server, httpAddr string, isDebug bool) error {
	if isDebug {
		fmt.Printf("Starting MCP server using Streamable HTTP transport on %s\n", httpAddr)
	}

	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return srv
	}, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			r = r.WithContext(context.WithValue(r.Context(), CtxAuthKey{}, auth))
		}
		mcpHandler.ServeHTTP(w, r)
	})

	if isDebug {
		fmt.Printf("Streamable HTTP Endpoint: http://localhost:%s/mcp\n", httpAddr)
	}

	httpSrv := &http.Server{
		Addr:    ":" + httpAddr,
		Handler: handler,
	}

	if err := httpSrv.ListenAndServe(); err != nil {
		return fmt.Errorf("streamable HTTP server error: %w", err)
	}

	return nil
}

// serveStdio starts the server in stdio mode
func serveStdio(srv *mcp.Server) error {
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("MCP server terminated: %w", err)
	}
	return nil
}
