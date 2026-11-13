// Package graphql queries the GraphQL API of an execution client.
package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Query runs query against the GraphQL endpoint at url and returns its data.
// A query that returns any GraphQL error fails.
func Query(ctx context.Context, url, query string) (map[string]any, error) {
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	// gqrl answers a query that has errors with 400 Bad Request and the errors.
	var result struct {
		Data   map[string]any    `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("query %s: %s: %w", query, response.Status, err)
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("query %s: %s", query, result.Errors)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query %s: %s", query, response.Status)
	}
	return result.Data, nil
}
