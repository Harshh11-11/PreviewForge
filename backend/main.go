package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/rs/cors"
)

type PreviewRequest struct {
	RepoURL string `json:"repoUrl"`
}

type PreviewResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

var (
	previewsMu sync.RWMutex
	previews   = map[string]int{}
)

func previewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	id := generateID()
	cloneDir := filepath.Join(os.TempDir(), "forge", id)
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		http.Error(w, "Failed to prepare workspace", http.StatusInternalServerError)
		return
	}

	go func() {
		log.Println("Cloning:", req.RepoURL)
		cmd := exec.Command("git", "clone", req.RepoURL, cloneDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Println("Clone failed:", string(out))
			return
		}
		stack := detectStack(cloneDir)
		log.Println("Detected stack:", stack)
		port, err := buildAndRun(id, cloneDir, stack)
		if err != nil {
			log.Println("Build error:", err)
			return
		}
		previewsMu.Lock()
		previews[id] = port
		previewsMu.Unlock()
		log.Printf("Preview ready: http://localhost:%d\n", port)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PreviewResponse{
		ID:      id,
		Status:  "cloning",
		Message: "Cloning " + req.RepoURL,
	})
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	port, ok := previews[id]
	w.Header().Set("Content-Type", "application/json")
	if !ok {
		json.NewEncoder(w).Encode(map[string]string{"status": "building"})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ready",
		"url":    fmt.Sprintf("http://localhost:%d", port),
	})

	// cleanup after serving once
	go func() {
		time.Sleep(1 * time.Minute) // give iframe time to load
		cloneDir := "/tmp/forge/" + id
		log.Println("Auto-cleaning:", cloneDir)
		os.RemoveAll(cloneDir)
		delete(previews, id)
	}()
}

func detectStack(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "unknown"
	}
	names := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		names[e.Name()] = struct{}{}
	}

	if _, ok := names["next.config.js"]; ok {
		return "nextjs"
	}
	if _, ok := names["next.config.mjs"]; ok {
		return "nextjs"
	}
	if _, ok := names["next.config.ts"]; ok {
		return "nextjs"
	}
	if _, ok := names["package.json"]; ok {
		return "node"
	}
	return "unknown"
}

func generateID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// last resort; still deterministic enough for dev
		return fmt.Sprintf("%08x", []byte("forge")[:4])
	}
	return fmt.Sprintf("%02x%02x%02x%02x", b[0], b[1], b[2], b[3])
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/preview", previewHandler)
	mux.HandleFunc("/api/status", statusHandler)

	handler := cors.New(cors.Options{
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"POST", "GET", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}).Handler(mux)

	log.Println("Backend running on :4000")
	log.Fatal(http.ListenAndServe(":4000", handler))
}