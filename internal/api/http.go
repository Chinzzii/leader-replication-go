// internal/api/http.go
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Chinzzii/leader-replication-go/internal/cluster"
	"github.com/Chinzzii/leader-replication-go/internal/repl"
	"github.com/Chinzzii/leader-replication-go/internal/store"

	"github.com/google/uuid" // Used for generating request IDs
)

// Server holds all dependencies for the HTTP API.
type Server struct {
	cfg    *cluster.NodeConfig // This node's configuration
	store  *store.KV           // The in-memory data store
	client *http.Client        // HTTP client for replicating to followers
	log    *log.Logger         // Structured logger
	mu     sync.Mutex          // Protects stateful operations (e.g., BlockPeers)
}

// PutRequest is the JSON body for a client's write request.
type PutRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// PutResponse is the JSON response to a client's write request.
type PutResponse struct {
	Status string `json:"status"` // "ok"
	Mode   string `json:"mode"`   // "sync" or "async"
	ReqID  string `json:"req_id"` // Unique ID for this request
}

// Status is the JSON response for the /status endpoint.
type Status struct {
	ID      string                 `json:"id"`
	Role    string                 `json:"role"`
	Mode    string                 `json:"mode"`
	Port    int                    `json:"port"`
	Peers   []string               `json:"peers"`
	Data    map[string]store.Entry `json:"data"`    // A snapshot of the store
	Blocked map[string]bool        `json:"blocked"` // List of blocked peers
}

// NewServer creates a new API server instance.
func NewServer(cfg *cluster.NodeConfig, kv *store.KV, logger *log.Logger) *Server {
	return &Server{
		cfg:   cfg,
		store: kv, // Assign the key-value store
		client: &http.Client{ // Create a client for replication
			Timeout: 5 * time.Second, // Always set timeouts!
		},
		log: logger, // Assign the logger
		// mu is usable as its zero-value
	}
}

// Routes sets up all HTTP handlers for the server.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Client-facing endpoints
	mux.HandleFunc("/put", s.handlePut) // Write a value (leader only)
	mux.HandleFunc("/get", s.handleGet) // Read a value (leader or follower)

	// Internal cluster endpoint
	mux.HandleFunc("/replicate", s.handleReplicate) // Receive replicated data (follower only)

	// Admin/status endpoints
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/partition", s.handlePartition) // Simulate network partitions

	return mux
}

// --- Client-Facing Handlers ---

// handlePut handles a client's write request.
// It is only processed by the leader.
func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Only the leader can accept writes.
	if s.cfg.Role != cluster.Leader {
		s.respondError(w, http.StatusForbidden, "not a leader")
		return
	}

	// 1. Decode the client's request.
	var req PutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Key == "" {
		s.respondError(w, http.StatusBadRequest, "key cannot be empty")
		return
	}

	// 2. Generate a unique ID and timestamp for this write.
	reqID := uuid.NewString()
	entry := store.Entry{
		Key:   req.Key,
		Value: req.Value,
		TS:    time.Now().UTC(),
	}

	// 3. Write to the leader's own store.
	s.store.Upsert(entry)
	s.log.Printf("[ReqID %s] leader local upsert: %s=%s", reqID, entry.Key, entry.Value)

	// 4. Create the replication request for followers.
	replReq := repl.ReplicateRequest{
		Key:   entry.Key,
		Value: entry.Value,
		TS:    entry.TS,
		ReqID: reqID,
	}

	// 5. Broadcast to followers based on the mode (sync or async).
	if s.cfg.Mode == cluster.Async {
		// Asynchronous: respond to client immediately and replicate in the background.
		go s.broadcastReplication(replReq)
		s.respondJSON(w, http.StatusOK, PutResponse{
			Status: "ok",
			Mode:   string(s.cfg.Mode),
			ReqID:  reqID,
		})
	} else {
		// Synchronous: block until replication is complete.
		s.broadcastReplication(replReq)
		s.respondJSON(w, http.StatusOK, PutResponse{
			Status: "ok",
			Mode:   string(s.cfg.Mode),
			ReqID:  reqID,
		})
	}
}

// handleGet handles a client's read request.
// This can be served by either a leader or a follower.
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		s.respondError(w, http.StatusBadRequest, "missing key query param")
		return
	}

	entry, ok := s.store.Get(key)
	if !ok {
		s.respondError(w, http.StatusNotFound, "key not found")
		return
	}

	s.respondJSON(w, http.StatusOK, entry)
}

// --- Internal Cluster Handlers ---

// handleReplicate is the endpoint followers expose for the leader.
// It receives a write and applies it to the follower's local store.
func (s *Server) handleReplicate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Followers should not replicate to other nodes.
	if s.cfg.Role == cluster.Leader {
		s.respondError(w, http.StatusForbidden, "leader cannot replicate to itself")
		return
	}

	// 1. Decode the leader's replication request.
	var req repl.ReplicateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid replication body")
		return
	}

	s.log.Printf("[ReqID %s] follower replicating: %s=%s", req.ReqID, req.Key, req.Value)

	// 2. Create the store Entry from the request.
	entry := store.Entry{
		Key:   req.Key,
		Value: req.Value,
		TS:    req.TS,
	}

	// 3. Upsert to the local store using LWW logic.
	s.store.Upsert(entry)

	// 4. Acknowledge the write.
	s.respondJSON(w, http.StatusOK, repl.ReplicateResponse{Status: "ok"})
}

// --- Admin & Status Handlers ---

// handleStatus returns the current state of the node.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Use a lock to safely read the BlockPeers map
	s.mu.Lock()
	// Create a copy to avoid race conditions after unlocking
	blocked := make(map[string]bool, len(s.cfg.BlockPeers))
	for k, v := range s.cfg.BlockPeers {
		blocked[k] = v
	}
	s.mu.Unlock()

	status := Status{
		ID:      s.cfg.ID,
		Role:    string(s.cfg.Role),
		Mode:    string(s.cfg.Mode),
		Port:    s.cfg.Port,
		Peers:   s.cfg.Peers,
		Data:    s.store.Snapshot(), // Get a safe copy of the data
		Blocked: blocked,
	}

	s.respondJSON(w, http.StatusOK, status)
}

// handlePartition is a testing endpoint to simulate network partitions.
// Usage:
//
//	/partition?block=http://follower1:8081
//	/partition?unblock=http://follower1:8081
func (s *Server) handlePartition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	blockPeer := r.URL.Query().Get("block")
	unblockPeer := r.URL.Query().Get("unblock")

	s.mu.Lock()
	defer s.mu.Unlock()

	if blockPeer != "" {
		s.log.Printf("!!! blocking peer: %s", blockPeer)
		s.cfg.BlockPeers[blockPeer] = true
	}
	if unblockPeer != "" {
		s.log.Printf("!!! unblocking peer: %s", unblockPeer)
		delete(s.cfg.BlockPeers, unblockPeer)
	}

	s.respondJSON(w, http.StatusOK, s.cfg.BlockPeers)
}

// --- Helper Methods ---

// broadcastReplication sends the replication request to all configured peers.
// It respects the BlockPeers map for partition testing.
// In sync mode, it blocks. In async mode, it does not.
func (s *Server) broadcastReplication(req repl.ReplicateRequest) {
	s.log.Printf("[ReqID %s] broadcasting to %d peers", req.ReqID, len(s.cfg.Peers))

	// Use a WaitGroup to track all replication goroutines.
	// This is necessary for *both* sync and async, but in async,
	// we just don't wait on it. In a real system, you'd still
	// want to know when the async ops finish, e.g., for metrics.
	var wg sync.WaitGroup

	for _, peerURL := range s.cfg.Peers {
		wg.Add(1)
		// Launch a separate goroutine for each peer.
		go func(url string) {
			defer wg.Done()

			// Check if this peer is partitioned (blocked).
			s.mu.Lock()
			isBlocked := s.cfg.BlockPeers[url]
			s.mu.Unlock()

			if isBlocked {
				s.log.Printf("[ReqID %s] skipped replication to %s (blocked)", req.ReqID, url)
				return
			}

			// Send the replication request.
			err := repl.PostReplicate(s.client, url, req)
			if err != nil {
				s.log.Printf("[ReqID %s] ERROR replicating to %s: %v", req.ReqID, url, err)
			} else {
				s.log.Printf("[ReqID %s] replicated to %s successfully", req.ReqID, url)
			}
		}(peerURL)
	}

	// In sync mode, we block until all goroutines are done.
	if s.cfg.Mode == cluster.Sync {
		s.log.Printf("[ReqID %s] waiting for sync replication...", req.ReqID)
		wg.Wait()
		s.log.Printf("[ReqID %s] sync replication complete.", req.ReqID)
	}
}

// respondJSON is a helper to write a JSON response.
func (s *Server) respondJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if payload != nil {
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			// This error is tricky to handle, as we've already written the header.
			s.log.Printf("ERROR: failed to write json response: %v", err)
			http.Error(w, "failed to write json response", http.StatusInternalServerError)
		}
	}
}

// respondError is a helper to write a JSON error response.
func (s *Server) respondError(w http.ResponseWriter, code int, message string) {
	// Don't log 404s as server errors.
	if code != http.StatusNotFound {
		s.log.Printf("HTTP %d: %s", code, message)
	}

	type ErrorResponse struct {
		Error string `json:"error"`
	}
	s.respondJSON(w, code, ErrorResponse{Error: message})
}
