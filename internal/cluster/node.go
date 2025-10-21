// internal/cluster/node.go
package cluster

import (
	"fmt"
	"net/url"
	"strings"
)

// Role defines the role of a node in the cluster.
type Role string

const (
	Leader   Role = "leader"
	Follower Role = "follower"
)

// Mode defines the replication mode for the leader.
type Mode string

const (
	Sync  Mode = "sync"  // Leader waits for followers to ACK before responding to client.
	Async Mode = "async" // Leader responds to client immediately.
)

// NodeConfig holds all configuration for a single node.
type NodeConfig struct {
	ID    string   // Unique ID for this node (e.g., "leader-1")
	Role  Role     // This node's role (leader or follower)
	Mode  Mode     // Replication mode (sync or async), only used by leader
	Port  int      // HTTP port this node listens on
	Peers []string // List of peer base URLs (e.g., "http://follower1:8081")

	// BlockPeers is a map used to simulate network partitions for testing.
	// If a peer's URL is a key in this map, the leader will not send
	// replication requests to it.
	BlockPeers map[string]bool
}

// BaseURL returns the local base URL for this node.
func (c *NodeConfig) BaseURL() string {
	// Assumes localhost; for container networking, this might differ.
	return fmt.Sprintf("http://localhost:%d", c.Port)
}

// NormalizePeers takes a comma-separated string of peer addresses
// and cleans it up into a slice of valid base URLs.
func NormalizePeers(peersCSV string) []string {
	if strings.TrimSpace(peersCSV) == "" {
		return nil
	}
	parts := strings.Split(peersCSV, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Try to parse as a full URL
		u, err := url.Parse(p)
		if err == nil && u.Scheme != "" && u.Host != "" {
			// It's a valid, full URL (e.g., "http://foo.com:8080")
			out = append(out, u.String())
		} else {
			// It's likely just "host:port", assume http.
			out = append(out, fmt.Sprintf("http://%s", p))
		}
	}
	return out
}
