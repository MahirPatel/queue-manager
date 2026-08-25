package handlers

import (
	"encoding/json"
	"net/http"
	"sync"

	"go.mongodb.org/mongo-driver/mongo"

	"queue-manager/internal/config"
	"queue-manager/internal/worker"
)

// Server holds the shared dependencies every handler method needs.
// Lifecycle is guarded by a mutex since it's set once at startup but read
// concurrently by every HTTP handler.
type Server struct {
	DB     *mongo.Database
	Config config.Config

	mu        sync.RWMutex
	lifecycle worker.Lifecycle
}

func (s *Server) SetLifecycle(l worker.Lifecycle) {
	s.mu.Lock()
	s.lifecycle = l
	s.mu.Unlock()
}

func (s *Server) Lifecycle() worker.Lifecycle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lifecycle
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
