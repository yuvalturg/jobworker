package manager

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	readBufferSize    = 4 << 10
	eventChannelSize  = 1 << 10
	outputChannelSize = 1 << 10
)

type watchObject struct {
	watchID      string
	watchFD      int32
	filePath     string
	eventChannel chan uint32
	outChannel   chan []byte
}

type isActiveFunc func() bool
type cleanupFunc func(*watchObject) error

func newWatchObject(inotifyFd int, filePath string) (*watchObject, error) {
	mask := unix.IN_OPEN | unix.IN_MODIFY | unix.IN_CLOSE_WRITE
	watchFd, err := unix.InotifyAddWatch(inotifyFd, filePath, uint32(mask))
	if err != nil {
		return nil, fmt.Errorf("failed adding watch for %s: %w", filePath, err)
	}

	watchObj := &watchObject{
		watchID:      uuid.NewString(),
		watchFD:      int32(watchFd),
		filePath:     filePath,
		outChannel:   make(chan []byte, outputChannelSize),
		eventChannel: make(chan uint32, eventChannelSize),
	}

	return watchObj, nil
}

func (o *watchObject) startWatching(isActive isActiveFunc, cleanup cleanupFunc) error {
	// At this point, this watch is already registered in inotify and
	// should be registered in LogWatcher as well.
	// This means that when we open the file, an IN_OPEN is immediately
	// triggered and its event is put on the eventChannel so we do not lose
	// events.
	file, err := os.Open(o.filePath)
	if err != nil {
		return fmt.Errorf("failed opening log file %s: %w", o.filePath, err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	buffer := make([]byte, readBufferSize)

	var once sync.Once

	for range o.eventChannel {
		if err := o.readToEOF(reader, buffer); err != nil {
			return fmt.Errorf("readToEOF failed: %w", err)
		}
		if !isActive() {
			once.Do(func() {
				cleanup(o)
			})
		}
	}

	log.Printf("Exiting watcher routine for [%s]", o.watchID)

	return nil
}

func (o *watchObject) readToEOF(reader *bufio.Reader, buffer []byte) error {
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			sendBuf := make([]byte, n)
			copy(sendBuf, buffer)
			select {
			case o.outChannel <- sendBuf:
			default:
				log.Printf("outChannel [%s] full, dropping buffer", o.watchID)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
