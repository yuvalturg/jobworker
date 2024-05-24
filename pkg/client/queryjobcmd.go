package client

import (
	"context"
	"flag"
	"fmt"
	pb "jobworker/pkg/api"
	"log"
)

type QueryJobCommand struct {
	*commonCommand
}

func NewQueryJobCommand() *QueryJobCommand {
	cmd := &QueryJobCommand{
		commonCommand: &commonCommand{
			fs: flag.NewFlagSet("status", flag.ExitOnError),
		},
	}

	cmd.addCommonFlags()
	return cmd
}

func (c *QueryJobCommand) Run(ctx context.Context) ([]byte, error) {
	log.Printf("Executing query command with args=%v", c.fs.Args())

	if len(c.fs.Args()) == 0 {
		return nil, fmt.Errorf("missing argument jobId")
	}

	req := pb.JobRequest{
		JobId: c.fs.Args()[0],
	}

	resp, err := c.client.QueryJob(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("error querying job: %w", err)
	}

	return marshalPrintJobResponse(resp)
}
