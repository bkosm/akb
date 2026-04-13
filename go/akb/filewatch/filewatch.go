package filewatch

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// OnFile is called for each matching file — on initial discovery and on
// create/modify/delete events. name is the relative path without the suffix,
// slash-separated. deleted is true when the file was removed or renamed away.
type OnFile func(name, path string, deleted bool)

// Register cleans and expands dir, walks it to call onFile for every existing
// file matching suffix, then starts an fsnotify watcher that calls onFile for
// future create/modify/delete events. Returns a stop func to tear down the
// watcher.
func Register(dir, suffix string, onFile OnFile) (func(), error) {
	dir = filepath.Clean(os.ExpandEnv(dir))

	if err := walkExisting(dir, suffix, onFile); err != nil {
		return nil, err
	}

	w, err := newWatcher(dir, suffix, onFile)
	if err != nil {
		slog.Warn("filewatch: watcher failed, files won't auto-update", "dir", dir, "err", err)
		return func() {}, nil
	}
	return w.stop, nil
}

func walkExisting(dir, suffix string, onFile OnFile) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if !strings.HasSuffix(d.Name(), suffix) {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		name := filepath.ToSlash(strings.TrimSuffix(rel, suffix))
		onFile(name, path, false)
		return nil
	})
}

type watcher struct {
	fsw     *fsnotify.Watcher
	rootDir string
	suffix  string
	onFile  OnFile
	done    chan struct{}
}

func newWatcher(dir, suffix string, onFile OnFile) (*watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &watcher{
		fsw:     fsw,
		rootDir: dir,
		suffix:  suffix,
		onFile:  onFile,
		done:    make(chan struct{}),
	}

	if err := w.addRecursive(dir); err != nil {
		_ = fsw.Close()
		return nil, err
	}

	go w.loop()
	return w, nil
}

func (w *watcher) addRecursive(dir string) error {
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

func (w *watcher) loop() {
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
			slog.Error("filewatch: watcher error", "err", err)
		}
	}
}

func (w *watcher) handle(ev fsnotify.Event) {
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
	if strings.HasPrefix(filepath.Base(path), ".") {
		return
	}

	rel, err := filepath.Rel(w.rootDir, path)
	if err != nil {
		return
	}
	name := filepath.ToSlash(strings.TrimSuffix(rel, w.suffix))

	if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
		w.onFile(name, path, true)
		return
	}

	if ev.Has(fsnotify.Create) || ev.Has(fsnotify.Write) {
		w.onFile(name, path, false)
	}
}

func (w *watcher) stop() {
	_ = w.fsw.Close()
	<-w.done
}
