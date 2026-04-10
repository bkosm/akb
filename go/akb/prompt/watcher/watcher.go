package watcher

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// Event describes a file change matching the watched suffix.
type Event struct {
	// Name is the relative path from the watch root without the suffix, slash-separated.
	Name string
	// Path is the absolute file path.
	Path string
	// Deleted is true when the file was removed or renamed away.
	Deleted bool
}

// Callback is invoked for each matching file change.
type Callback func(Event)

// Watcher monitors a directory tree for file changes matching a suffix.
type Watcher struct {
	fsw     *fsnotify.Watcher
	rootDir string
	suffix  string
	cb      Callback
	done    chan struct{}
}

// Watch starts monitoring dir recursively for files matching suffix.
// The callback is invoked for create, modify, and delete events.
// Call Stop to release resources.
func Watch(dir, suffix string, cb Callback) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		fsw:     fsw,
		rootDir: dir,
		suffix:  suffix,
		cb:      cb,
		done:    make(chan struct{}),
	}

	if err := w.addRecursive(dir); err != nil {
		_ = fsw.Close()
		return nil, err
	}

	go w.loop()
	return w, nil
}

func (w *Watcher) addRecursive(dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return w.fsw.Add(path)
		}
		return nil
	})
}

func (w *Watcher) loop() {
	defer close(w.done)
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("file watcher error: %v", err)
		}
	}
}

func (w *Watcher) handle(ev fsnotify.Event) {
	path := ev.Name

	if ev.Has(fsnotify.Create) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			_ = w.addRecursive(path)
			return
		}
	}

	if !strings.HasSuffix(path, w.suffix) {
		return
	}

	rel, err := filepath.Rel(w.rootDir, path)
	if err != nil {
		return
	}
	name := filepath.ToSlash(strings.TrimSuffix(rel, w.suffix))

	if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
		w.cb(Event{Name: name, Path: path, Deleted: true})
		return
	}

	if ev.Has(fsnotify.Create) || ev.Has(fsnotify.Write) {
		w.cb(Event{Name: name, Path: path})
	}
}

// Stop closes the watcher and waits for the event loop to exit.
func (w *Watcher) Stop() {
	_ = w.fsw.Close()
	<-w.done
}
