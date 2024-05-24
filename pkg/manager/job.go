package manager

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	cgroupSysFsRoot              = "/sys/fs/cgroup"
	jobWorkerManagerLogDir       = "/tmp/jobworker"
	jobWorkerLogDirPerms         = 0o755
	jobWorkerCPUMaxQuotaMicroSec = 500_000
	jobWorkerMemMaxBytes         = 500_000
	jobWorkerIoMaxBps            = 500_000
)

type JobStatus int32

const (
	JobInit JobStatus = iota
	JobScheduled
	JobFailedToStart
	JobRunning
	JobStopped
)

func (s JobStatus) String() string {
	return [...]string{"Init", "Scheduled", "FailedToStart", "Running", "Stopped"}[s]
}

// These JobOptions are used for testing only.
type JobOption func(*Job)

func WithCloneFlags(flags uintptr) JobOption {
	return func(c *Job) {
		c.cloneFlags = flags
	}
}

func WithCgroup(cgroup *Cgroup) JobOption {
	return func(c *Job) {
		c.cgroup = cgroup
	}
}

type JobInfo struct {
	jobID    string
	pid      atomic.Int32
	exitCode atomic.Int32
	command  string
	args     []string
	status   atomic.Int32
}

func (j *JobInfo) JobID() string {
	return j.jobID
}

func (j *JobInfo) Status() JobStatus {
	return JobStatus(j.status.Load())
}

func (j *JobInfo) ExitCode() int32 {
	return j.exitCode.Load()
}

func (j *JobInfo) ProcessID() int32 {
	return j.pid.Load()
}

type Job struct {
	*JobInfo
	logFile    *os.File
	cancelFunc context.CancelFunc
	// cloneFlags and cgroup are modified in tests only
	cloneFlags uintptr
	cgroup     *Cgroup
}

func NewJob(command string, args []string, opts ...JobOption) (*Job, error) {
	jobID := uuid.NewString()

	ret := &Job{
		JobInfo: &JobInfo{
			jobID:   jobID,
			command: command,
			args:    args,
		},
		cloneFlags: unix.CLONE_NEWNS | unix.CLONE_NEWPID | unix.CLONE_NEWNET,
		cgroup:     NewCgroup(cgroupSysFsRoot, jobID),
	}

	for _, opt := range opts {
		opt(ret)
	}

	return ret, nil
}

// start:
// - Prepares the job (cgroup, logfile and context).
// - Executes the command.
// - Start a monitoring goroutine.
func (j *Job) start(ctx context.Context) error {
	if swapped := j.status.CompareAndSwap(int32(JobInit), int32(JobScheduled)); !swapped {
		return fmt.Errorf("invalid initial status for %s", j.jobID)
	}

	if err := j.initCgroup(); err != nil {
		j.stop(JobScheduled, JobFailedToStart)
		return fmt.Errorf("failed initializing cgroup for job %s: %w", j.jobID, err)
	}

	// logFile will look like $jobWorkerManagerLogDir/$jobId.log
	if err := j.openLogFile(); err != nil {
		j.stop(JobScheduled, JobFailedToStart)
		return fmt.Errorf("failed opening logfile: %w", err)
	}

	// Execute and redirect stdout and std err to logfile.
	log.Printf("Executing job: %v cgrp=%v", j, j.cgroup)

	// Save the context cancelation function in job in case
	// we would like to stop it
	var cmdCtx context.Context
	cmdCtx, j.cancelFunc = context.WithCancel(ctx)

	// Prepare the command and its attributes
	cmd := exec.CommandContext(cmdCtx, j.command, j.args...)
	cmd.Stdout = j.logFile
	cmd.Stderr = j.logFile

	// Execute the process in new namespaces if applicable
	attrs := &unix.SysProcAttr{
		Cloneflags: j.cloneFlags,
		Setpgid:    true,
	}

	// Execute the process with the constraints of the new cgroup
	if j.cgroup != nil {
		attrs.UseCgroupFD = true
		attrs.CgroupFD = j.cgroup.fd
	}

	cmd.SysProcAttr = attrs

	// Starts running the job
	if err := cmd.Start(); err != nil {
		j.stop(JobScheduled, JobFailedToStart)
		return fmt.Errorf("failed starting command for %s: %w", j.jobID, err)
	}

	log.Printf("Registering pid=%d for job %s", cmd.Process.Pid, j.jobID)
	j.pid.Store(int32(cmd.Process.Pid))
	j.status.Store(int32(JobRunning))

	// Start a goroutine to monitor the process
	go j.monitorCommand(cmd)

	return nil
}

// initCgroup:
// - Creates the cgroup (mkdir $cgroupPath/$name).
// - Sets the limits for the cgroup according to job.limit.
func (j *Job) initCgroup() error {
	// This should be nil in `go test` so we won't need root priviledges
	if j.cgroup == nil {
		return nil
	}

	limits := &ResourceLimits{
		CPUMaxQuotaMicroSec: jobWorkerCPUMaxQuotaMicroSec,
		MemMaxBytes:         jobWorkerMemMaxBytes,
		IOMaxBytesPerSec:    jobWorkerIoMaxBps,
	}

	log.Printf("Initializing cgroup with limits %v", limits)

	if err := j.cgroup.Create(limits); err != nil {
		return fmt.Errorf("failed creating cgroup: %w", err)
	}

	return nil
}

// monitorCommand:
// - Runs in a goroutine.
// - Waits for the command to finish.
// - Registers the exitCode.
// - Cleans up the job (deletes cgroups, closes files etc).
func (j *Job) monitorCommand(cmd *exec.Cmd) {
	err := cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	j.exitCode.Store(int32(exitCode))

	log.Printf("Job cmd.Wait for %s returned %v, exitCode=%d", j.jobID, err, exitCode)
	// The process ended somehow, either gracefully or by calling its cancelFunc.
	// We need to clean up its resources (mainly cgroup), update its status to stopped,
	// and close the file.  The close file event will trigger an inotify CLOSE_WRITE
	// event which in turn will close the the stream's outputChannel

	err = j.stop(JobRunning, JobStopped)
	log.Printf("Job stop for %s returned %v", j.jobID, err)
}

func (j *Job) openLogFile() error {
	// ensure logdir exists
	if err := os.MkdirAll(jobWorkerManagerLogDir, jobWorkerLogDirPerms); err != nil {
		return fmt.Errorf("failed creating log directory %s: %w", jobWorkerManagerLogDir, err)
	}

	// open our log file
	logPath := filepath.Join(jobWorkerManagerLogDir, j.jobID+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to create logfile %s: %w", logPath, err)
	}
	j.logFile = logFile

	return nil
}

func (j *Job) stop(oldStatus, status JobStatus) error {
	if swapped := j.status.CompareAndSwap(int32(oldStatus), int32(status)); !swapped {
		return fmt.Errorf("unexpcted status for job %s: %v", oldStatus, status)
	}

	if j.logFile != nil {
		if err := j.logFile.Close(); err != nil {
			return fmt.Errorf("failed closing logfile: %w", err)
		}
	}

	if j.cgroup != nil {
		if err := j.cgroup.Delete(); err != nil {
			return fmt.Errorf("deleting cgroup for job failed: %w", err)
		}
	}

	return nil
}

func (j *Job) isActive() bool {
	status := j.Status()
	return status == JobRunning || status == JobScheduled
}
