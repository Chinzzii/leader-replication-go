// cmd/server/main.go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/Chinzzii/leader-replication-go/internal/api"
	"github.com/Chinzzii/leader-replication-go/internal/cluster"
	"github.com/Chinzzii/leader-replication-go/internal/store"
)

func main() {
	// --- Configuration via command-line flags ---
	var (
		id    = flag.String("id", "node-1", "node id")
		role  = flag.String("role", "leader", "leader|follower")
		mode  = flag.String("mode", "sync", "sync|async (leader only)")
		port  = flag.Int("port", 8080, "http port")
		peers = flag.String("peers", "", "comma-separated peer baseURLs (followers for leader)")
	)
	flag.Parse()

	// --- Build Node Configuration ---
	cfg := &cluster.NodeConfig{
		ID:    *id,
		Role:  cluster.Role(*role),
		Mode:  cluster.Mode(*mode),
		Port:  *port,
		Peers: cluster.NormalizePeers(*peers), // Parse the CSV string
		// BlockPeers map is initialized empty by default.
		BlockPeers: map[string]bool{},
	}

	// --- Initialize Dependencies ---
	kv := store.New()
	// Create a standard logger
	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", cfg.ID), log.LstdFlags)

	// Pass all dependencies to the server constructor
	server := api.NewServer(cfg, kv, logger)

	// --- Start Server ---
	addr := fmt.Sprintf(":%d", cfg.Port)
	logger.Printf("starting %s %s on %s mode=%s peers=%v", cfg.Role, cfg.ID, addr, cfg.Mode, cfg.Peers)

	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		logger.Fatal(err)
	}
}
