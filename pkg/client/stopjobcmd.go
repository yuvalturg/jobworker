package client

import (
	"context"
	"flag"
	"fmt"
	pb "jobworker/pkg/api"
	"log"
)

type StopJobCommand struct {
	*commonCommand
}

func NewStopJobCommand() *StopJobCommand {
	cmd := &StopJobCommand{
		commonCommand: &commonCommand{
			fs: flag.NewFlagSet("stop", flag.ExitOnError),
		},
	}

	cmd.addCommonFlags()
	return cmd
}

func (c *StopJobCommand) Run(ctx context.Context) ([]byte, error) {
	log.Printf("Executing stop command with args=%v", c.fs.Args())

	if len(c.fs.Args()) == 0 {
		return nil, fmt.Errorf("missing argument jobId")
	}

	req := pb.JobRequest{
		JobId: c.fs.Args()[0],
	}

	resp, err := c.client.StopJob(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("error stopping job: %w", err)
	}

	return marshalPrintJobResponse(resp)
}
