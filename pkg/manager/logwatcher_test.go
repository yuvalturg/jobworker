package manager

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
)

func watchOutput(watcher *LogWatcher, filePath string, isActive isActiveFunc, expected string) error {
	outChannel, err := watcher.AddWatch(filePath, isActive)
	if err != nil {
		return fmt.Errorf("failed adding watch: %w", err)
	}

	output := ""
	for b := range outChannel {
		line := string(b)
		output += line
	}

	if output != expected {
		return fmt.Errorf("wrong output, expected=[%s], received=[%s]", expected, output)
	}

	log.Printf("Output matched: %q", output)

	return nil
}

func TestLogWatcher(t *testing.T) {
	t.Parallel()

	watcher, err := NewLogWatcher()
	if err != nil {
		t.Fatalf("failed to initialize logwatcher: %v", err)
	}

	tmpfile, err := os.CreateTemp("", "logfile-")
	if err != nil {
		t.Fatalf("failed creating tmp")
	}

	start := 1
	end := 5
	expected := ""
	for i := 1; i <= 5; i++ {
		expected += fmt.Sprintf("%d\n", i)
	}
	cmd := exec.Command("bash", "-c", fmt.Sprintf("for x in {%d..%d}; do echo $x; sleep 1; done", start, end))

	cmd.Stdout = tmpfile

	if err = cmd.Start(); err != nil {
		t.Fatalf("failed starting command: %v", err)
	}

	var active atomic.Bool
	active.Store(true)

	go func() {
		cmd.Wait()
		tmpfile.Close()
		active.Store(false)
	}()

	isActive := func() bool {
		return active.Load()
	}

	watchCount := 5
	var errGrp errgroup.Group

	for i := 0; i < watchCount; i++ {
		foo := i
		errGrp.Go(func() error {
			time.Sleep(time.Duration(foo) * time.Second)
			return watchOutput(watcher, tmpfile.Name(), isActive, expected)
		})
	}

	if err = errGrp.Wait(); err != nil {
		t.Fatalf("watchers failed: %v", err)
	}

	if err := watcher.Close(); err != nil {
		t.Fatalf("watcher close failed: %v", err)
	}
}
