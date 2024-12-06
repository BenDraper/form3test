package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/form3tech-oss/interview-simulator/server"
)

const (
	gracePeriod = 3 * time.Second
	port        = 8080
)

func main() {
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	server, err := server.NewServer(fmt.Sprintf("localhost:%d", port), gracePeriod)
	if err != nil {
		fmt.Println("Error starting server:", err)
		return
	}

	go server.Serve()

	<-shutdown
	server.Shutdown()
}
