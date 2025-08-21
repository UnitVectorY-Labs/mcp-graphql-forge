package forge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
)

// ExecuteGraphQL posts a query+vars to url with Bearer token, returning raw JSON
func ExecuteGraphQL(url, query string, vars map[string]interface{}, token string, isDebug bool) ([]byte, error) {
	payload := GraphqlRequest{Query: query, Variables: vars}
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
		req.Header.Set("Authorization", token)
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
