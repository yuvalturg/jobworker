package server_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	pb "jobworker/pkg/api"
	"jobworker/pkg/manager"
	"jobworker/pkg/server"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func getClient(t *testing.T, clientName string) pb.JobWorkerClient {
	t.Helper()

	certDir := os.Getenv("JOBWORKER_SERVER_CERT_DIR")
	clientCert, err := tls.LoadX509KeyPair(
		filepath.Join(certDir, clientName+".crt"),
		filepath.Join(certDir, clientName+".key"),
	)
	if err != nil {
		t.Fatalf("failed to load client certificate and key. [%v]", err)
	}

	serverCA, err := os.ReadFile(filepath.Join(certDir, "ca.crt"))
	if err != nil {
		t.Fatalf("failed reading ca: %v", err)
	}

	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(serverCA) {
		t.Fatalf("failed to append ca to pool. [%v]", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      rootCAs,
	}

	port := os.Getenv("JOBWORKER_SERVER_PORT")
	conn, err := grpc.NewClient(
		fmt.Sprintf("localhost:%s", port),
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	)
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	return pb.NewJobWorkerClient(conn)
}

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

func checkStreamContains(cli pb.JobWorkerClient, jobID, expected string) error {
	stream, err := cli.StreamJob(context.Background(), &pb.JobRequest{JobId: jobID})
	if err != nil {
		return fmt.Errorf("StreamJob failed: %w", err)
	}

	output := ""
	for {
		data, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("receiving data failed: %w", err)
		}
		output += string(data.Message)
	}

	log.Printf("Streamed output: [%v]", output)

	if !strings.Contains(output, expected) {
		return fmt.Errorf("invalid output, expected to find %s, output=%s", expected, output)
	}

	return nil
}

func checkStatus(t *testing.T, cli pb.JobWorkerClient, jobID string, status manager.JobStatus) {
	t.Helper()

	res, err := cli.QueryJob(context.Background(), &pb.JobRequest{JobId: jobID})
	if err != nil {
		t.Fatalf("query failed")
	}

	if server.StatusMap[status] != res.Status {
		t.Fatalf("expected status %s, received status %s", status.String(), res.Status.String())
	}
}

func TestServerShortLivingJob(t *testing.T) {
	t.Parallel()

	srv := getServer(t, "4567")
	defer srv.Close()

	aliceClient := getClient(t, "alice")
	bobClient := getClient(t, "bob")

	res, err := aliceClient.StartJob(context.Background(), &pb.StartJobRequest{
		Command:   "ls",
		Arguments: []string{"-l", "/dev/null"},
	})
	if err != nil {
		t.Fatalf("failed calling StartJob: %v", err)
	}

	if err = checkStreamContains(aliceClient, res.JobId, "/dev/null"); err != nil {
		t.Fatalf("stream check failed: %v", err)
	}
	checkStatus(t, aliceClient, res.JobId, manager.JobStopped)

	// Query again but with a different client, and make sure we
	// get a permission denied
	_, err = bobClient.QueryJob(context.Background(), &pb.JobRequest{JobId: res.JobId})
	if err == nil {
		t.Fatalf("bob queried alice's job successfully")
	}
}

func TestServerLongRunningJob(t *testing.T) {
	t.Parallel()

	srv := getServer(t, "5678")
	defer srv.Close()

	cli := getClient(t, "alice")

	res, err := cli.StartJob(context.Background(), &pb.StartJobRequest{
		Command:   "bash",
		Arguments: []string{"-c", "while :; do echo hello; sleep 2; done"},
	})
	if err != nil {
		t.Fatalf("failed calling StartJob: %v", err)
	}

	checkStatus(t, cli, res.JobId, manager.JobRunning)

	dur := 3 * time.Second
	log.Printf("Sleeping %v", dur)
	time.Sleep(dur)

	_, err = cli.StopJob(context.Background(), &pb.JobRequest{JobId: res.JobId})
	if err != nil {
		t.Fatalf("Failed to stop job: %v", err)
	}

	checkStreamContains(cli, res.JobId, "hello")
	checkStatus(t, cli, res.JobId, manager.JobStopped)
}
