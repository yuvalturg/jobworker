package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	pb "jobworker/pkg/api"
	"jobworker/pkg/manager"
	"log"
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	StatusMap = map[manager.JobStatus]pb.JobStatus{
		manager.JobInit:          pb.JobStatus_jobInit,
		manager.JobScheduled:     pb.JobStatus_jobScheduled,
		manager.JobFailedToStart: pb.JobStatus_jobFailedToStart,
		manager.JobRunning:       pb.JobStatus_jobRunning,
		manager.JobStopped:       pb.JobStatus_jobStopped,
	}
)

type JobWorkerServer struct {
	pb.UnimplementedJobWorkerServer
	jobManager  *manager.JobManager
	authHandler *authHandler
	grpcServer  *grpc.Server
}

func NewJobWorkerServer() (*JobWorkerServer, error) {
	mgr, err := manager.NewJobManager()
	if err != nil {
		return nil, fmt.Errorf("failed creating manager: %w", err)
	}
	return &JobWorkerServer{
		jobManager:  mgr,
		authHandler: newAuthHandler(),
	}, nil
}

// Serve -
// - Load certificates
// - Register the gprc server
// - Start listening on the given port
func (s *JobWorkerServer) Serve() error {
	serverCertDir := getEnvWithDefault("JOBWORKER_SERVER_CERT_DIR", "certs")
	serverPort := getEnvWithDefault("JOBWORKER_SERVER_PORT", "5678")

	serverCert, err := tls.LoadX509KeyPair(
		filepath.Join(serverCertDir, "server.crt"),
		filepath.Join(serverCertDir, "server.key"),
	)
	if err != nil {
		return fmt.Errorf("failed to certificates: %w", err)
	}

	clientCA, err := os.ReadFile(filepath.Join(serverCertDir, "ca.crt"))
	if err != nil {
		return fmt.Errorf("failed to load client CA certificate: %w", err)
	}

	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(clientCA) {
		return fmt.Errorf("faile to append CA cert to pool: %w", err)
	}

	// Create the TLS configuration
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    clientCAs,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	listener, err := net.Listen("tcp", ":"+serverPort)
	if err != nil {
		return fmt.Errorf("failed listening on port %s: %w", serverPort, err)
	}

	s.grpcServer = grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
	)

	pb.RegisterJobWorkerServer(s.grpcServer, s)

	log.Printf("Server listening on port %s", serverPort)

	if err := s.grpcServer.Serve(listener); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

// Closes the server
func (s *JobWorkerServer) Close() {
	if s.grpcServer != nil {
		s.grpcServer.Stop()
	}
}

// StartJob:
// - Validates peer certificate
// - Starts a new job in the manager
func (s *JobWorkerServer) StartJob(ctx context.Context, req *pb.StartJobRequest) (*pb.JobResponse, error) {
	owner, err := s.authHandler.startJobAllowed(ctx)
	if err != nil {
		return &pb.JobResponse{}, err
	}

	log.Printf("StartJob: %v", req)

	var jobOpts []manager.JobOption
	if getEnvWithDefault("JOBWORKER_SERVER_TEST", "") != "" {
		jobOpts = append(jobOpts, manager.WithCgroup(nil), manager.WithCloneFlags(0))
	}

	jobInfo, err := s.jobManager.StartJob(context.Background(), req.Command, req.Arguments, jobOpts...)
	if err != nil {
		return &pb.JobResponse{}, err
	}

	s.authHandler.registerJobID(jobInfo.JobID(), owner)

	return jobResponseFromJobInfo(jobInfo), nil
}

// QueryJob:
// - Validates peer certificate
// - Fetches the job info from the manager
func (s *JobWorkerServer) QueryJob(ctx context.Context, req *pb.JobRequest) (*pb.JobResponse, error) {
	if err := s.authHandler.checkOwnership(ctx, req.JobId); err != nil {
		return &pb.JobResponse{}, err
	}

	jobInfo, err := s.jobManager.QueryJob(req.JobId)
	if err != nil {
		return &pb.JobResponse{}, err
	}

	return jobResponseFromJobInfo(jobInfo), err
}

// StopJob:
// - Validates peer certificate
// - Stops a a job in the manager
func (s *JobWorkerServer) StopJob(ctx context.Context, req *pb.JobRequest) (*pb.JobResponse, error) {
	if err := s.authHandler.checkOwnership(ctx, req.JobId); err != nil {
		return &pb.JobResponse{}, err
	}

	jobInfo, err := s.jobManager.StopJob(req.JobId)
	if err != nil {
		return &pb.JobResponse{}, err
	}

	return jobResponseFromJobInfo(jobInfo), err
}

// StreamJob:
// - Validates peer certificate
// - Requests stream from the manager
// - Reads from the channel provided by the manager and streams the received data
func (s *JobWorkerServer) StreamJob(req *pb.JobRequest, stream pb.JobWorker_StreamJobServer) error {
	if err := s.authHandler.checkOwnership(stream.Context(), req.JobId); err != nil {
		return err
	}

	outChannel, err := s.jobManager.StreamJob(req.JobId)
	if err != nil {
		return fmt.Errorf("failed calling manager stream for %s: %w", req.JobId, err)
	}

	for data := range outChannel {
		res := pb.StreamJobResponse{
			Message: data,
		}
		if err := stream.Send(&res); err != nil {
			return fmt.Errorf("failed sending output %s: %w", req.JobId, err)
		}
	}

	return nil
}

// This is for fetching certificates dir and server port
func getEnvWithDefault(envVar, defultVal string) string {
	if val, ok := os.LookupEnv(envVar); ok {
		return val
	}

	return defultVal
}

func jobResponseFromJobInfo(jobInfo *manager.JobInfo) *pb.JobResponse {
	return &pb.JobResponse{
		JobId:    jobInfo.JobID(),
		Pid:      jobInfo.ProcessID(),
		ExitCode: jobInfo.ExitCode(),
		Status:   StatusMap[jobInfo.Status()],
	}
}
