package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
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

	install := exec.Command("npm", "install")
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

	// Wait until port is actually open
	for i := 0; i < 30; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)

		if err == nil {
			conn.Close()
			log.Println("Server is up!")
			return port, nil
		}

		log.Println("Waiting...", i+1)
		time.Sleep(2 * time.Second)
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
