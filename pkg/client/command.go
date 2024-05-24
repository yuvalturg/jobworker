package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	pb "jobworker/pkg/api"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	defaultServerAddress = "localhost:5678"
)

type command interface {
	Name() string
	Init([]string) error
	Run(context.Context) ([]byte, error)
}

type commonCommand struct {
	fs         *flag.FlagSet
	serverAddr string
	caFile     string
	keyFile    string
	certFile   string
	conn       *grpc.ClientConn
	client     pb.JobWorkerClient
}

// Sets up common flags for all commands
func (c *commonCommand) addCommonFlags() {
	c.fs.StringVar(&c.serverAddr, "server-addr", defaultServerAddress, "Server address url")
	c.fs.StringVar(&c.caFile, "ca", "certs/ca.crt", "Path to ca cert file")
	c.fs.StringVar(&c.keyFile, "key", "certs/alice.key", "Path to client key file")
	c.fs.StringVar(&c.certFile, "cert", "certs/alice.crt", "Path to client cert file")
}

func (c *commonCommand) Name() string {
	return c.fs.Name()
}

// Parses command line arguments and creates a connection
func (c *commonCommand) Init(args []string) error {
	if err := c.fs.Parse(args); err != nil {
		return fmt.Errorf("failed parsing args: %w", err)
	}

	if err := c.initGrpcClient(); err != nil {
		return fmt.Errorf("failed initializing jobworker client: %w", err)
	}

	return nil
}

// Loads the client's certificates, creates a connection and client
func (c *commonCommand) initGrpcClient() error {
	clientCert, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		return fmt.Errorf("failed to load client certificate and key. %w", err)
	}

	serverCA, err := os.ReadFile(c.caFile)
	if err != nil {
		return fmt.Errorf("failed to load trusted certificate. %w", err)
	}

	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(serverCA) {
		return fmt.Errorf("failed to append trusted certificate to certificate pool. %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      rootCAs,
	}

	cred := credentials.NewTLS(tlsConfig)

	conn, err := grpc.NewClient(c.serverAddr, grpc.WithTransportCredentials(cred))
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.conn = conn
	c.client = pb.NewJobWorkerClient(conn)

	return nil
}

// Executes the command itself and returns the output as bytes.
//
//	Some examples to execute the commands are:
//
// ./jobclient start -- ls -l /dev/null
// ./jobclient stream $jobID
func ExecuteCommand(ctx context.Context, args []string) ([]byte, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("you must pass a sub-command")
	}

	cmds := []command{
		NewStartJobCommand(),
		NewQueryJobCommand(),
		NewStopJobCommand(),
		NewStreamJobCommand(),
	}

	subcommand := args[0]

	for _, cmd := range cmds {
		if cmd.Name() == subcommand {
			if err := cmd.Init(args[1:]); err != nil {
				return nil, fmt.Errorf("failed initializing command %w", err)
			}
			return cmd.Run(ctx)
		}
	}

	return nil, fmt.Errorf("unknown subcommand: %s", subcommand)
}

// Marshals a JobResponse to json bytes and prints it as string
func marshalPrintJobResponse(response *pb.JobResponse) ([]byte, error) {
	data, err := protojson.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("could not marshal response: %w", err)
	}

	// For
	fmt.Print(string(data))

	return data, nil
}
