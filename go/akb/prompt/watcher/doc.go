// Package watcher provides a recursive fsnotify-based file watcher that
// monitors a directory tree for files matching a given suffix.
//
// Watch starts a watcher on the given root directory and delivers Event values
// (with the file's KB-relative name, path, and a Deleted flag) to a callback
// whenever a matching file is created, written, or removed. The returned Watcher
// must be stopped via its Stop method to release fsnotify resources.
package watcher
