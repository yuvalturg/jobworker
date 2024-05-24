package manager

import (
	"context"
	"fmt"
	"sync"
)

// JobManager is the main struct for the package.
// jobDB is our in memory database, it looks like {"jobID" : *Job}
type JobManager struct {
	jobDB   sync.Map
	watcher *LogWatcher
}

func NewJobManager() (*JobManager, error) {
	watcher, err := NewLogWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize log watcher: %w", err)
	}

	return &JobManager{
		watcher: watcher,
	}, nil
}

// StartJob:
//   - Stores the job in our db
//   - Runs the job
func (m *JobManager) StartJob(ctx context.Context, command string, args []string, opts ...JobOption) (*JobInfo, error) {
	job, err := NewJob(command, args, opts...)
	if err != nil {
		return nil, fmt.Errorf("could not create job: %w", err)
	}

	// Make sure we didn't call StartJob on this job already
	if _, loaded := m.jobDB.LoadOrStore(job.jobID, job); loaded {
		return nil, fmt.Errorf("cannot reuse job id %s", job.JobID())
	}

	if err := job.start(ctx); err != nil {
		return nil, fmt.Errorf("job %s failed to start: %w", job.jobID, err)
	}

	return job.JobInfo, nil
}

// StopJob:
//   - Loads the job by its jobID
//   - Calls the command's context cancelFunc
func (m *JobManager) StopJob(jobID string) (*JobInfo, error) {
	j, ok := m.jobDB.Load(jobID)
	if !ok {
		return nil, fmt.Errorf("job %s was not found in memory", jobID)
	}

	job, ok := j.(*Job)
	if !ok {
		return nil, fmt.Errorf("type assertion failed for job %s", jobID)
	}

	// Cancel the command's context which will kill the process
	job.cancelFunc()

	return job.JobInfo, nil
}

// QueryJob:
//   - Loads the job by jobID
//   - Returns the job's status
func (m *JobManager) QueryJob(jobID string) (*JobInfo, error) {
	j, ok := m.jobDB.Load(jobID)
	if !ok {
		return nil, fmt.Errorf("job %s was not found in memory", jobID)
	}

	job, ok := j.(*Job)
	if !ok {
		return nil, fmt.Errorf("type assertion failed for job %s", jobID)
	}

	return job.JobInfo, nil
}

// StreamJob:
//   - Loads the job by jobID
//   - Adds the job's log file to the logwatcher
//   - Wait for the job to stop by reading from the job's doneChannel.
//     When the job stops, its monitor goroutine will push a struct to
//     its doneChannel.
//   - Once the job is done, we remove the watch from the logwatcher.
func (m *JobManager) StreamJob(jobID string) (<-chan []byte, error) {
	j, ok := m.jobDB.Load(jobID)
	if !ok {
		return nil, fmt.Errorf("job %s was not found in memory", jobID)
	}

	job, ok := j.(*Job)
	if !ok {
		return nil, fmt.Errorf("type assertion failed for job %s", jobID)
	}

	return m.watcher.AddWatch(job.logFile.Name(), job.isActive)
}
