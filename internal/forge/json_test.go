package forge

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecuteGraphQLJSON(t *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		requests <- string(body)
		_, _ = w.Write([]byte(`{"data":null}`))
	}))
	defer server.Close()

	if _, err := ExecuteGraphQL(server.URL, "query { x }", nil, "", false); err != nil {
		t.Fatal(err)
	}
	if got, want := <-requests, `{"query":"query { x }"}`; got != want {
		t.Fatalf("request = %s, want %s", got, want)
	}
	if _, err := ExecuteGraphQL(server.URL, string([]byte{0xff}), nil, "", false); err == nil {
		t.Fatal("expected invalid UTF-8 in request to fail")
	}
}

func TestProcessOutputJSONV2(t *testing.T) {
	tests := []struct {
		name, input, output, want string
	}{
		{"json minimized", ` { "data" : [null, 1] } `, "json", `{"data":[null,1]}`},
		{"duplicate names", ` { "data": 1, "data": 2 } `, "json", ` { "data": 1, "data": 2 } `},
		{"duplicate names toon", ` { "data": 1, "data": 2 } `, "toon", ` { "data": 1, "data": 2 } `},
		{"invalid UTF-8", "{\"data\":\"\xff\"}", "json", "{\"data\":\"\xff\"}"},
		{"raw unchanged", ` { "data": 1, "data": 2 } `, "raw", ` { "data": 1, "data": 2 } `},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := processOutput([]byte(tt.input), tt.output, false); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExecuteGraphQLRejectsInvalidVariables(t *testing.T) {
	_, err := ExecuteGraphQL("http://example.invalid", "query", map[string]any{"x": string([]byte{0xff})}, "", false)
	if err == nil || !strings.Contains(err.Error(), "marshal GraphQL payload") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}
