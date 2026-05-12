package backup

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCreateAndReplaceContents(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "note.md"), "hello")
	writeFile(t, filepath.Join(root, ".hidden"), "secret")
	if err := os.Symlink("docs/note.md", filepath.Join(root, "note-link")); err != nil {
		t.Fatal(err)
	}

	archivePath, err := Create(root, time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC), KindNormal)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := filepath.Base(archivePath), filepath.Base(root)+".20260512-130000.backup.tar.gz"; got != want {
		t.Fatalf("archive filename = %q, want %q", got, want)
	}

	writeFile(t, filepath.Join(root, "docs", "note.md"), "changed")
	writeFile(t, filepath.Join(root, "extra.txt"), "remove me")
	if err := ReplaceContents(root, archivePath); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, filepath.Join(root, "docs", "note.md")); got != "hello" {
		t.Fatalf("restored note = %q, want hello", got)
	}
	if got := readFile(t, filepath.Join(root, ".hidden")); got != "secret" {
		t.Fatalf("restored hidden file = %q, want secret", got)
	}
	if _, err := os.Stat(filepath.Join(root, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("extra file should be removed, err=%v", err)
	}
	link, err := os.Readlink(filepath.Join(root, "note-link"))
	if err != nil {
		t.Fatal(err)
	}
	if link != "docs/note.md" {
		t.Fatalf("symlink target = %q, want docs/note.md", link)
	}
}

func TestCreateSkipsMetadataArtifacts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.txt"), "keep")
	writeFile(t, filepath.Join(root, ".DS_Store"), "skip")
	writeFile(t, filepath.Join(root, "._keep.txt"), "skip")

	archivePath, err := Create(root, time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC), KindNormal)
	if err != nil {
		t.Fatal(err)
	}

	restoreRoot := filepath.Join(t.TempDir(), "restore")
	if err := os.MkdirAll(restoreRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RestoreInto(restoreRoot, archivePath); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(restoreRoot, "keep.txt")); got != "keep" {
		t.Fatalf("keep.txt = %q, want keep", got)
	}
	for _, name := range []string{".DS_Store", "._keep.txt"} {
		if _, err := os.Stat(filepath.Join(restoreRoot, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should not be restored, err=%v", name, err)
		}
	}
}

func TestPreRestoreArchivePathUsesDedicatedSuffix(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "kb")
	if got, want := filepath.Base(ArchivePath(root, time.Date(2026, 5, 12, 13, 1, 2, 0, time.UTC), KindPreRestore)), "kb.20260512-130102.pre-restore.backup.tar.gz"; got != want {
		t.Fatalf("pre-restore filename = %q, want %q", got, want)
	}
}

func TestListLatestAndPrune(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "kb")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, ts := range []string{"20260512-130000", "20260512-130100", "20260512-130200"} {
		writeFile(t, filepath.Join(dir, "kb."+ts+".backup.tar.gz"), "")
	}
	writeFile(t, filepath.Join(dir, "kb.20260512-130300.pre-restore.backup.tar.gz"), "")
	writeFile(t, filepath.Join(dir, "other.20260512-130400.backup.tar.gz"), "")

	archives, err := List(root, KindNormal)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, archive := range archives {
		names = append(names, filepath.Base(archive.Path))
	}
	want := []string{
		"kb.20260512-130200.backup.tar.gz",
		"kb.20260512-130100.backup.tar.gz",
		"kb.20260512-130000.backup.tar.gz",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("archives = %#v, want %#v", names, want)
	}
	latest, err := Latest(root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(latest.Path) != "kb.20260512-130200.backup.tar.gz" {
		t.Fatalf("latest = %q", latest.Path)
	}

	removed, err := Prune(root, KindNormal, 2)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "kb.20260512-130000.backup.tar.gz")); !os.IsNotExist(err) {
		t.Fatalf("old archive should be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "kb.20260512-130300.pre-restore.backup.tar.gz")); err != nil {
		t.Fatalf("pre-restore archive should remain: %v", err)
	}
}

func TestRestoreRejectsUnsafePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	archivePath := maliciousArchive(t, "../escape.txt", "bad")

	if err := RestoreInto(root, archivePath); err == nil {
		t.Fatal("expected unsafe path error")
	}
}

func TestRestoreRejectsEscapingSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	archivePath := symlinkArchive(t, "link", "../outside")

	if err := RestoreInto(root, archivePath); err == nil {
		t.Fatal("expected unsafe symlink error")
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func maliciousArchive(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bad.backup.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzw := gzip.NewWriter(file)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(contents))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(contents)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func symlinkArchive(t *testing.T, name, target string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bad-link.backup.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzw := gzip.NewWriter(file)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{Name: name, Linkname: target, Typeflag: tar.TypeSymlink, Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
