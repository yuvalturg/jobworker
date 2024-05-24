package main

import (
	"jobworker/pkg/server"
	"log"
)

func main() {
	s, err := server.NewJobWorkerServer()
	if err != nil {
		log.Fatalf("Failed creating server: [%v]", err)
	}

	if err := s.Serve(); err != nil {
		log.Fatalf("Failed to serve: [%v]", err)
	}
}
