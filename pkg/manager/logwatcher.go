package manager

import (
	"fmt"
	"log"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The LogWatcher is the part of the manager that is responsible
// for streaming log files to a channel.  It uses inotify and
// works similarly to `tail -f`, meaning once we add a file to
// our list of watched files, it will try to stream data until
// this file is removed from the list.
// watchObjMap is a map between { "watchID": *watchObject }
type LogWatcher struct {
	inotifyFD   int
	watchObjMu  sync.RWMutex
	watchObjMap map[int32][]*watchObject
}

func NewLogWatcher() (*LogWatcher, error) {
	// We initialize a single file descriptor for reading inotify events
	fd, err := unix.InotifyInit()
	if err != nil {
		return nil, fmt.Errorf("failed initializing inotify: %w", err)
	}

	watcher := &LogWatcher{
		inotifyFD:   fd,
		watchObjMap: make(map[int32][]*watchObject),
	}

	// This goroutine reads inotify events for registered files and
	// in case a file is modified, its content will be read and streamed
	go watcher.processEvents()

	return watcher, nil
}

// AddWatch:
//   - Creates a watchObject for the given `filePath`.
//   - Registers the watchObject with inotify fd
//   - Opens the file and reads its full content, the file is kept opened
//     as long as we're streaming in order to read from the same position
func (w *LogWatcher) AddWatch(filePath string, isActive isActiveFunc) (<-chan []byte, error) {
	w.watchObjMu.Lock()
	defer w.watchObjMu.Unlock()

	// Register a new watchObject, and open the file
	watchObj, err := newWatchObject(w.inotifyFD, filePath)
	if err != nil {
		return nil, fmt.Errorf("could not create watch object for %s: %w", filePath, err)
	}

	// Register the watchObject in a map so that the processor can access
	// the opened file object and outputChannel
	log.Printf("Start watch [%s] on %s (fd=%d)", watchObj.watchID, filePath, watchObj.watchFD)

	w.watchObjMap[watchObj.watchFD] = append(w.watchObjMap[watchObj.watchFD], watchObj)

	go watchObj.startWatching(isActive, w.removeWatchObject)

	return watchObj.outChannel, nil
}

// Close:
//   - closes the main inotify file descriptor
func (w *LogWatcher) Close() error {
	log.Printf("Closing log watcher")
	return unix.Close(w.inotifyFD)
}

// processEvents:
//   - Runs in the background
//   - Reads inotify events from the main inotify file descriptor
//   - Calls readToEOF which will read the file and send the output
//     to watchObject's outputChannel
func (w *LogWatcher) processEvents() {
	log.Printf("Start processing inotify events")

	buf := make([]byte, unix.SizeofInotifyEvent)

	for {
		n, err := unix.Read(w.inotifyFD, buf)
		if err != nil {
			log.Printf("Inotify read returned %v", err)
			break
		}

		for offset := 0; offset < n; {
			event := (*unix.InotifyEvent)(unsafe.Pointer(&buf[offset]))

			if event.Mask&unix.IN_IGNORED == 0 {
				w.processSingleEvent(event)
			}

			offset += int(event.Len) + unix.SizeofInotifyEvent
		}
	}
}

func (w *LogWatcher) processSingleEvent(event *unix.InotifyEvent) {
	w.watchObjMu.RLock()
	defer w.watchObjMu.RUnlock()

	watchObjects, ok := w.watchObjMap[event.Wd]
	if !ok {
		return
	}

	for _, watchObj := range watchObjects {
		select {
		case watchObj.eventChannel <- event.Mask:
		default:
		}
	}
}

func (w *LogWatcher) removeWatchObject(watchObj *watchObject) error {
	w.watchObjMu.Lock()
	defer w.watchObjMu.Unlock()

	log.Printf("Removing watch ID %s", watchObj.watchID)

	watchObjects, ok := w.watchObjMap[watchObj.watchFD]
	if !ok {
		return fmt.Errorf("watch fd %d not found", watchObj.watchFD)
	}

	for i := 0; i < len(watchObjects); {
		if watchObjects[i].watchID == watchObj.watchID {
			watchObjects = append(watchObjects[:i], watchObjects[i+1:]...)
			continue
		}
		i++
	}

	close(watchObj.outChannel)
	close(watchObj.eventChannel)

	if len(watchObjects) == 0 {
		log.Printf("Removing watch fd %d", watchObj.watchFD)
		_, err := unix.InotifyRmWatch(w.inotifyFD, uint32(watchObj.watchFD))
		if err != nil {
			return fmt.Errorf("inotify rm watch failed: %w", err)
		}
	}

	return nil
}
