package manager

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	cpuMaxMicroSec     = 1_000_000
	ioMaxMountPoint    = "/"
	procMountsPath     = "/proc/self/mounts"
	cgroupDirPerm      = 0o755
	cgroupFilePerm     = 0o644
	numProcMountFields = 6
)

type ResourceLimits struct {
	CPUMaxQuotaMicroSec int64
	MemMaxBytes         int64
	IOMaxBytesPerSec    int64
}

type Cgroup struct {
	fd   int
	root string
	path string
}

func NewCgroup(root, name string) *Cgroup {
	return &Cgroup{
		fd:   -1,
		root: root,
		path: filepath.Join(root, name),
	}
}

// Create:
// - Activates the cpu, memory and io controllers.
// - Mkdir /sys/fs/cgroup/$cgroup-name.
// - Open a descriptor to the new cgroup.
func (c *Cgroup) Create(limits *ResourceLimits) error {
	// Make sure controllers are activated
	controllers := "+cpu +memory +io"
	if err := writeToFilename(filepath.Join(c.root, "cgroup.subtree_control"), controllers); err != nil {
		return fmt.Errorf("failed activating cgroup controllers: %w", err)
	}

	if _, err := os.Stat(c.path); os.IsNotExist(err) {
		if err := os.Mkdir(c.path, cgroupDirPerm); err != nil {
			return fmt.Errorf("failed creating cgroup path %s: %w", c.path, err)
		}
	}

	file, err := os.OpenFile(c.path, unix.O_PATH, 0)
	if err != nil {
		return fmt.Errorf("failed opening cgroup path %s: %w", c.path, err)
	}
	c.fd = int(file.Fd())

	if err = c.setLimits(limits); err != nil {
		return fmt.Errorf("failed setting cgroup limits: %w", err)
	}

	return nil
}

// Delete:
// - Close the cgroup file descriptor.
// - Delete the cgroup.
func (c *Cgroup) Delete() error {
	if c.fd > 0 {
		log.Print("Closing cgroup file descriptor")

		if err := unix.Close(c.fd); err != nil {
			return fmt.Errorf("failed closing cgroup fd for %s: %w", c.path, err)
		}
	}

	log.Printf("Deleting cgroup path %s", c.path)

	if err := os.RemoveAll(c.path); err != nil {
		return fmt.Errorf("failed removing cgroup %s: %w", c.path, err)
	}

	return nil
}

func (c *Cgroup) setLimits(limits *ResourceLimits) error {
	if limits.CPUMaxQuotaMicroSec > 0 {
		if err := c.setCPULimit(limits.CPUMaxQuotaMicroSec); err != nil {
			return fmt.Errorf("failed setting cpu limit: %w", err)
		}
	}

	if limits.IOMaxBytesPerSec > 0 {
		if err := c.setDiskIOLimit(limits.IOMaxBytesPerSec); err != nil {
			return fmt.Errorf("failed setting disk io limit: %w", err)
		}
	}

	if limits.MemMaxBytes > 0 {
		if err := c.setMemoryLimit(limits.MemMaxBytes); err != nil {
			return fmt.Errorf("failed setting memory limit: %w", err)
		}
	}

	return nil
}

// setCPULimit:
// - Using a fixed period `cpuMaxMicroSec` calculate the quota
// - Write quota and period to cpu.max
func (c *Cgroup) setCPULimit(limit int64) error {
	value := fmt.Sprintf("%d %d", limit, cpuMaxMicroSec)

	return writeToFilename(filepath.Join(c.path, "cpu.max"), value)
}

// setMemoryLimit:
// - Write limit to memory.max.
func (c *Cgroup) setMemoryLimit(limit int64) error {
	value := strconv.FormatInt(limit, 10)

	return writeToFilename(filepath.Join(c.path, "memory.max"), value)
}

// setDiskIOLimit:
// - Find the device for /.
// - Find the device's major and minor numbers.
// - Write rbps and wbps values to io.max file.
func (c *Cgroup) setDiskIOLimit(limit int64) error {
	device, err := getDeviceForMount(ioMaxMountPoint)
	if err != nil {
		return fmt.Errorf("failed getting device for mount: %w", err)
	}

	var stat unix.Stat_t
	if err = unix.Stat(device, &stat); err != nil {
		return fmt.Errorf("error calling stat on device %s: %w", device, err)
	}

	major := unix.Major(stat.Rdev)

	log.Printf("Found device=%v, major=%v for %s", device, major, ioMaxMountPoint)

	// We found the major and minor, but we will just use the major to limit
	// access to the entire disk regardless of partitions
	value := fmt.Sprintf("%v:0 rbps=%d wbps=%d", major, limit, limit)

	return writeToFilename(filepath.Join(c.path, "io.max"), value)
}

func writeToFilename(path, value string) error {
	if err := os.WriteFile(path, []byte(value), cgroupFilePerm); err != nil {
		return fmt.Errorf("could not write to %s: %w", path, err)
	}

	return nil
}

func getDeviceForMount(mountpoint string) (string, error) {
	file, err := os.Open(procMountsPath)
	if err != nil {
		return "", fmt.Errorf("failed opening file: %w", err)
	}
	defer file.Close()

	// A proc mounts file takes the following format:
	// <device> <mount> <fstype> <fsoptions> <dump> <passno>
	// An example for the file looks like:
	// /dev/nvme0n1p4 / btrfs rw,seclabel,relatime,compress=zstd:1,ssd,discard=async,space_cache=v2,subvolid=256,subvol=/root 0 0
	// We need to return the device for given mountpoint
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != numProcMountFields {
			continue
		}
		if fields[1] == mountpoint {
			return fields[0], nil
		}
	}

	return "", fmt.Errorf("mountpoint %s not found", mountpoint)
}
