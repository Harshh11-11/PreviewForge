package main

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"time"
)

func buildAndRun(id string, cloneDir string, stack string) (int, error) {
	port := 5173

	log.Println("Installing dependencies...")
	install := exec.Command("npm", "install")
	install.Dir = cloneDir
	out, err := install.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("npm install failed: %s", string(out))
	}

	log.Println("Starting vite on port:", port)
	run := exec.Command("npm", "run", "dev", "--", "--port", fmt.Sprintf("%d", port), "--host")
	run.Dir = cloneDir
	err = run.Start()
	if err != nil {
		return 0, fmt.Errorf("start failed: %s", err)
	}

	// wait until port is actually open
	for i := 0; i < 30; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), time.Second)
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