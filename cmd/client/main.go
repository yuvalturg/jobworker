package main

import (
	"context"
	"log"
	"os"

	"jobworker/pkg/client"
)

func main() {
	_, err := client.ExecuteCommand(context.Background(), os.Args[1:])
	if err != nil {
		log.Fatalf("Executing command failed: %v", err)
	}
}
