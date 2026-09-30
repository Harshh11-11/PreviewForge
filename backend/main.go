package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	previews   = map[string]previewJob{}
)

type previewJob struct {
	status           string
	port             int
	message          string
	cleanupScheduled bool
}

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

	if req.RepoURL == "" {
		http.Error(w, "repoUrl is required", http.StatusBadRequest)
		return
	}

	id := generateID()

	previewsMu.Lock()
	previews[id] = previewJob{
		status: "cloning",
	}
	previewsMu.Unlock()

	cloneDir := filepath.Join(os.TempDir(), "forge", id)

	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		setPreviewJob(id, previewJob{
			status:  "failed",
			message: "Failed to prepare workspace",
		})

		http.Error(
			w,
			"Failed to prepare workspace",
			http.StatusInternalServerError,
		)
		return
	}

	go func() {
		log.Println("Cloning:", req.RepoURL)

		cmd := exec.Command(
			"git",
			"-c",
			"credential.helper=",
			"clone",
			req.RepoURL,
			cloneDir,
		)

		out, err := cmd.CombinedOutput()

		if err != nil {
			log.Println("Clone failed:", string(out))

			setPreviewJob(id, previewJob{
				status:  "failed",
				message: "Repository clone failed",
			})

			os.RemoveAll(cloneDir)
			return
		}

		stack := detectStack(cloneDir)

		log.Println("Detected stack:", stack)

		setPreviewJob(id, previewJob{
			status: "building",
		})

		port, err := buildAndRun(id, cloneDir, stack)

		if err != nil {
			log.Println("Build error:", err)

			setPreviewJob(id, previewJob{
				status:  "failed",
				message: "Preview build failed",
			})

			os.RemoveAll(cloneDir)
			return
		}

		setPreviewJob(id, previewJob{
			status: "ready",
			port:   port,
		})

		log.Printf(
			"Preview ready on port %d",
			port,
		)
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

	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	previewsMu.RLock()
	job, ok := previews[id]
	previewsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	if !ok {
		w.WriteHeader(http.StatusNotFound)

		json.NewEncoder(w).Encode(map[string]string{
			"status": "unknown",
		})

		return
	}

	if job.status == "failed" {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "failed",
			"message": job.message,
		})

		return
	}

	if job.status != "ready" {
		json.NewEncoder(w).Encode(map[string]string{
			"status": job.status,
		})

		return
	}

	baseURL := os.Getenv("PUBLIC_BASE_URL")

	if baseURL == "" {
		baseURL = "https://previewforge-backend.onrender.com"
	}

	baseURL = strings.TrimRight(baseURL, "/")

	previewURL := fmt.Sprintf(
		"%s/preview/%s/",
		baseURL,
		id,
	)

	json.NewEncoder(w).Encode(map[string]string{
		"status": "ready",
		"url":    previewURL,
	})

	// Schedule cleanup only once.
	previewsMu.Lock()

	current := previews[id]

	scheduleCleanup := !current.cleanupScheduled

	if scheduleCleanup {
		current.cleanupScheduled = true
		previews[id] = current
	}

	previewsMu.Unlock()

	if !scheduleCleanup {
		return
	}

	go func() {
		time.Sleep(30 * time.Minute)

		cloneDir := filepath.Join(
			os.TempDir(),
			"forge",
			id,
		)

		log.Println("Auto-cleaning:", cloneDir)

		if err := os.RemoveAll(cloneDir); err != nil {
			log.Println("Preview cleanup failed:", err)
		}

		previewsMu.Lock()
		delete(previews, id)
		previewsMu.Unlock()
	}()
}

func previewProxyHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(
		r.URL.Path,
		"/preview/",
	)

	parts := strings.SplitN(path, "/", 2)

	id := parts[0]

	if id == "" {
		http.Error(
			w,
			"preview id is required",
			http.StatusBadRequest,
		)
		return
	}

	previewsMu.RLock()
	job, ok := previews[id]
	previewsMu.RUnlock()

	if !ok || job.status != "ready" {
		http.Error(
			w,
			"preview not found or not ready",
			http.StatusNotFound,
		)
		return
	}

	target, err := url.Parse(
		fmt.Sprintf(
			"http://127.0.0.1:%d",
			job.port,
		),
	)

	if err != nil {
		http.Error(
			w,
			"invalid preview target",
			http.StatusInternalServerError,
		)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director

	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Remove /preview/{id} from the path
		// before forwarding to Vite.

		req.URL.Path = "/"

		if len(parts) == 2 && parts[1] != "" {
			req.URL.Path = "/" + parts[1]
		}

		req.Host = target.Host
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		contentType := resp.Header.Get("Content-Type")

		if strings.Contains(contentType, "text/html") {
			resp.Header.Set(
				"Cache-Control",
				"no-store",
			)
		}

		return nil
	}

	proxy.ErrorHandler = func(
		w http.ResponseWriter,
		r *http.Request,
		err error,
	) {
		log.Println(
			"Preview proxy error:",
			err,
		)

		http.Error(
			w,
			"Preview server unavailable",
			http.StatusBadGateway,
		)
	}

	proxy.ServeHTTP(w, r)
}

func setPreviewJob(id string, job previewJob) {
	previewsMu.Lock()
	previews[id] = job
	previewsMu.Unlock()
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
		return fmt.Sprintf(
			"%08x",
			[]byte("forge")[:4],
		)
	}

	return fmt.Sprintf(
		"%02x%02x%02x%02x",
		b[0],
		b[1],
		b[2],
		b[3],
	)
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc(
		"/api/preview",
		previewHandler,
	)

	mux.HandleFunc(
		"/api/status",
		statusHandler,
	)

	mux.HandleFunc(
		"/preview/",
		previewProxyHandler,
	)

	handler := cors.New(cors.Options{
		AllowedOrigins: []string{
			"http://localhost:3000",
			"https://preview-forge.vercel.app",
		},

		AllowedMethods: []string{
			"POST",
			"GET",
			"OPTIONS",
		},

		AllowedHeaders: []string{
			"Content-Type",
		},
	}).Handler(mux)

	port := os.Getenv("PORT")

	if port == "" {
		port = "4000"
	}

	log.Printf(
		"Backend running on :%s",
		port,
	)

	log.Fatal(
		http.ListenAndServe(
			":"+port,
			handler,
		),
	)
}