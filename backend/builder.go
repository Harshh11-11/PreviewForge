package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func buildAndRun(id string, cloneDir string, stack string) (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate preview port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	log.Println("Installing dependencies...")

	installArgs := []string{"install", "--no-audit", "--no-fund"}
	if _, err := os.Stat(filepath.Join(cloneDir, "package-lock.json")); err == nil {
		installArgs = []string{"ci", "--no-audit", "--no-fund"}
	}
	install := exec.Command("npm", installArgs...)
	install.Dir = cloneDir

	out, err := install.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("npm install failed: %s", string(out))
	}

	log.Println("Starting preview server on port:", port)
	run := previewCommand(cloneDir, stack, port)

	run.Dir = cloneDir

	// Show the preview server's output in backend logs.
	run.Stdout = os.Stdout
	run.Stderr = os.Stderr

	err = run.Start()
	if err != nil {
		return 0, fmt.Errorf("start failed: %s", err)
	}

	// A listening socket can still return a blank/error page while the app compiles.
	// Wait for an actual HTTP response before exposing the preview to the iframe.
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < http.StatusInternalServerError {
				log.Println("Preview is responding over HTTP")
				return port, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return 0, fmt.Errorf("server never started on port %d", port)
}

func previewCommand(dir string, stack string, port int) *exec.Cmd {
	portArg := fmt.Sprintf("%d", port)
	if stack == "nextjs" {
		return exec.Command("npm", "run", "dev", "--", "--hostname", "0.0.0.0", "--port", portArg)
	}

	// Honor a repository's dev script when present. Vite is the fallback for
	// simple frontend repositories without one.
	var packageJSON struct {
		Scripts map[string]string `json:"scripts"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err == nil && json.Unmarshal(data, &packageJSON) == nil && packageJSON.Scripts["dev"] != "" {
		return exec.Command("npm", "run", "dev", "--", "--host", "0.0.0.0", "--port", portArg)
	}
	return exec.Command("npx", "vite", "--host", "0.0.0.0", "--port", portArg, "--strictPort")
}
