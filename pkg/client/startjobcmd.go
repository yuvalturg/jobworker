package client

import (
	"context"
	"errors"
	"flag"
	"fmt"
	pb "jobworker/pkg/api"
	"log"
)

type StartJobCommand struct {
	*commonCommand
}

func NewStartJobCommand() *StartJobCommand {
	cmd := &StartJobCommand{
		commonCommand: &commonCommand{
			fs: flag.NewFlagSet("start", flag.ExitOnError),
		},
	}

	cmd.addCommonFlags()

	return cmd
}

func (c *StartJobCommand) Run(ctx context.Context) ([]byte, error) {
	log.Printf("Executing start command with args=%v flags=%v", c.fs.Args(), c)
	if len(c.fs.Args()) == 0 {
		return nil, errors.New("must provide command to start")
	}

	req := pb.StartJobRequest{
		Command:   c.fs.Args()[0],
		Arguments: c.fs.Args()[1:],
	}

	resp, err := c.client.StartJob(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("error starting job: %w", err)
	}

	return marshalPrintJobResponse(resp)
}
