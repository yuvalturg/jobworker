package client_test

import (
	"context"
	"fmt"
	pb "jobworker/pkg/api"
	"jobworker/pkg/client"
	"jobworker/pkg/server"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/encoding/protojson"
)

func getServer(t *testing.T, port string) *server.JobWorkerServer {
	t.Helper()

	os.Setenv("JOBWORKER_SERVER_TEST", "yes")
	os.Setenv("JOBWORKER_SERVER_CERT_DIR", "../../certs")
	os.Setenv("JOBWORKER_SERVER_PORT", port)

	srv, err := server.NewJobWorkerServer()
	if err != nil {
		t.Fatalf("failed creating server")
	}

	go srv.Serve()

	time.Sleep(time.Second)

	return srv
}

func getArgs(subcommand string, args []string) []string {
	ret := []string{
		subcommand,
		"-ca", "../../certs/ca.crt",
		"-cert", "../../certs/alice.crt",
		"-key", "../../certs/alice.key",
		"-server-addr", "localhost:6789",
	}
	return append(ret, args...)
}

func execCmdForJobResponse(t *testing.T, args []string) *pb.JobResponse {
	t.Helper()

	output, err := client.ExecuteCommand(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute command failed %v", err)
	}

	var jobResp pb.JobResponse
	if err := protojson.Unmarshal(output, &jobResp); err != nil {
		t.Fatalf("Unmarshal failed %v", err)
	}

	return &jobResp
}

func TestClient(t *testing.T) {
	t.Parallel()

	srv := getServer(t, "6789")
	defer srv.Close()

	args := getArgs("start", []string{"bash", "-c", "for x in {1..10}; do echo $x; sleep 1; done"})
	resp := execCmdForJobResponse(t, args)
	if resp.Status != pb.JobStatus_jobRunning {
		t.Fatalf("Job should be running, status=%v", resp.Status)
	}

	outputChannel := make(chan []byte, 1<<10)
	var grp errgroup.Group
	grp.Go(func() error {
		args = getArgs("stream", []string{resp.JobId})
		output, err := client.ExecuteCommand(context.Background(), args)
		if err != nil {
			return fmt.Errorf("execute command failed: %w", err)
		}
		outputChannel <- output
		return nil
	})

	time.Sleep(3 * time.Second)

	args = getArgs("stop", []string{resp.JobId})
	resp = execCmdForJobResponse(t, args)

	time.Sleep(time.Second)

	args = getArgs("status", []string{resp.JobId})
	resp = execCmdForJobResponse(t, args)
	if resp.Status != pb.JobStatus_jobStopped {
		t.Fatalf("Job should be stopped, status=%v", resp.Status)
	}

	if err := grp.Wait(); err != nil {
		t.Fatalf("stream goroutine failed: %v", err)
	}
	prefix := "1\n2\n3"
	output := <-outputChannel

	if !strings.HasPrefix(string(output), prefix) {
		t.Fatalf("Unexpected output=%s, expected=%s", string(output), prefix)
	}
}
