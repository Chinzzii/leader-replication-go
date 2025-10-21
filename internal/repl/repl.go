// internal/repl/repl.go
package repl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ReplicateRequest is the payload sent from a leader to a follower
// to replicate a single write operation.
type ReplicateRequest struct {
	Key   string    `json:"key"`
	Value string    `json:"value"`
	TS    time.Time `json:"ts"`
	ReqID string    `json:"req_id"` // ReqID for tracing/logging
}

// ReplicateResponse is the simple ACK response from a follower.
type ReplicateResponse struct {
	Status string `json:"status"` // e.g., "ok"
}

// PostReplicate sends a single replication request to a follower node.
// It takes a pre-configured http.Client, the base URL of the follower,
// and the request body.
func PostReplicate(client *http.Client, baseURL string, body ReplicateRequest) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal replicate request: %w", err)
	}

	// Construct the full URL, e.g., "http://follower1:8081/replicate"
	url := fmt.Sprintf("%s/replicate", baseURL)

	// Post the JSON payload.
	resp, err := client.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		// This handles network errors (e.g., connection refused)
		return fmt.Errorf("http post to %s failed: %w", url, err)
	}
	// Always close the response body to prevent resource leaks.
	defer resp.Body.Close()

	// Check for a non-2xx status code.
	if resp.StatusCode != http.StatusOK {
		// The follower received the request but rejected it.
		return fmt.Errorf("non-200 status from follower %s: %s", url, resp.Status)
	}

	return nil
}
