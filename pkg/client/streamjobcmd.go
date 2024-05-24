package client

import (
	"context"
	"flag"
	"fmt"
	"io"
	pb "jobworker/pkg/api"
	"log"
)

type StreamJobCommand struct {
	*commonCommand
}

func NewStreamJobCommand() *StreamJobCommand {
	cmd := &StreamJobCommand{
		commonCommand: &commonCommand{
			fs: flag.NewFlagSet("stream", flag.ExitOnError),
		},
	}

	cmd.addCommonFlags()
	return cmd
}

func (c *StreamJobCommand) Run(ctx context.Context) ([]byte, error) {
	log.Printf("Executing stream command with args=%v", c.fs.Args())

	if len(c.fs.Args()) == 0 {
		return nil, fmt.Errorf("missing argument jobId")
	}

	req := pb.JobRequest{
		JobId: c.fs.Args()[0],
	}

	stream, err := c.client.StreamJob(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("error querying job: %w", err)
	}

	var output []byte

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error while receiving data: %w", err)
		}

		fmt.Printf("%q", resp.Message)

		output = append(output, resp.Message...)
	}

	return output, nil
}
