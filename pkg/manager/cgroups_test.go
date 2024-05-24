package manager_test

import (
	"jobworker/pkg/manager"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func assertLineContent(t *testing.T, filePath, expectedRegex string) {
	t.Helper()

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed reading file")
	}

	re := regexp.MustCompile(expectedRegex)

	if !re.MatchString(string(data)) {
		t.Fatalf("line=[%s], regex=[%s]", string(data), expectedRegex)
	}
}

func TestCgroups(t *testing.T) {
	t.Parallel()

	tmpdir := t.TempDir()

	// We don't want to run the tests as root, so this just
	// will make sure the output file look ok
	limits := &manager.ResourceLimits{
		CPUMaxQuotaMicroSec: 100,
		MemMaxBytes:         200,
		IOMaxBytesPerSec:    300,
	}

	cgrp := manager.NewCgroup(tmpdir, "gizmo")
	if err := cgrp.Create(limits); err != nil {
		t.Fatalf("Failed creating cgroup %v: %v", cgrp, err)
	}

	assertLineContent(t, filepath.Join(tmpdir, "cgroup.subtree_control"), `^\+cpu \+memory \+io$`)
	assertLineContent(t, filepath.Join(tmpdir, "gizmo", "cpu.max"), "^100 1000000$")
	assertLineContent(t, filepath.Join(tmpdir, "gizmo", "memory.max"), "^200$")
	assertLineContent(t, filepath.Join(tmpdir, "gizmo", "io.max"), `^\d+:\d+ rbps=300 wbps=300$`)

	if err := cgrp.Delete(); err != nil {
		t.Fatalf("Failed delting cgroup: %v", err)
	}

	_, err := os.Stat(filepath.Join(tmpdir, "gizmo"))
	if !os.IsNotExist(err) {
		t.Fatalf("Cgroup was not deleted")
	}
}
