package manager_test

import (
	"context"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"jobworker/pkg/manager"

	"golang.org/x/sync/errgroup"
)

func checkStatus(t *testing.T, mgr *manager.JobManager, jobID string, expected manager.JobStatus) {
	t.Helper()

	jobInfo, err := mgr.QueryJob(jobID)
	if err != nil {
		t.Fatalf("Failed to query job: %v", err)
	}

	if jobInfo.Status() != expected {
		t.Fatalf("expected status %v, received status %v", expected, jobInfo.Status())
	}
}

func checkStreamContains(mgr *manager.JobManager, jobID, exptected string) error {
	outputChannel, err := mgr.StreamJob(jobID)
	if err != nil {
		return fmt.Errorf("failed to stream job: %w", err)
	}

	output := ""
	for line := range outputChannel {
		output += string(line[:len(line)-1])
	}

	log.Printf("Received output [%v]", output)

	if !strings.Contains(output, exptected) {
		return fmt.Errorf("expected to find %s in stream [%s]", exptected, output)
	}

	return nil
}

func TestShortRunningJob(t *testing.T) {
	t.Parallel()

	mgr, err := manager.NewJobManager()
	if err != nil {
		t.Fatalf("Failed creating manager: %v", err)
	}

	command := "ls"
	args := []string{"-l", "/dev/null"}

	job, err := mgr.StartJob(
		context.Background(),
		command,
		args,
		manager.WithCgroup(nil),
		manager.WithCloneFlags(0),
	)
	if err != nil {
		t.Fatalf("Failed starting job: %v", err)
	}

	if err := checkStreamContains(mgr, job.JobID(), "/dev/null"); err != nil {
		t.Fatalf("Check stream failed: %v", err)
	}

	checkStatus(t, mgr, job.JobID(), manager.JobStopped)
}

func TestLongRunningJob(t *testing.T) {
	t.Parallel()

	mgr, err := manager.NewJobManager()
	if err != nil {
		t.Fatalf("Failed creating manager: %v", err)
	}

	command := "bash"
	args := []string{"-c", "for x in {1..9}; do echo $x; sleep 1; done"}
	job, err := mgr.StartJob(
		context.Background(),
		command,
		args,
		manager.WithCgroup(nil),
		manager.WithCloneFlags(0),
	)
	if err != nil {
		t.Fatalf("Failed starting job: %v", err)
	}

	checkStatus(t, mgr, job.JobID(), manager.JobRunning)

	streamJobs := 3
	var errGrp errgroup.Group

	for i := 0; i < streamJobs; i++ {
		errGrp.Go(func() error {
			return checkStreamContains(mgr, job.JobID(), "1234")
		})
	}

	dur := 5 * time.Second
	log.Printf("Sleeping %v", dur)
	time.Sleep(dur)

	_, err = mgr.StopJob(job.JobID())
	if err != nil {
		t.Fatalf("Failed to stop job: %v", err)
	}

	if err = errGrp.Wait(); err != nil {
		t.Fatalf("streaming assertion failed: %v", err)
	}

	checkStatus(t, mgr, job.JobID(), manager.JobStopped)
}
