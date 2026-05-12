package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const timestampLayout = "20060102-150405"

// Kind describes why an archive was created.
type Kind int

const (
	KindNormal Kind = iota
	KindPreRestore
)

// Archive describes a timestamped sibling backup file.
type Archive struct {
	Path      string
	Timestamp time.Time
}

// ArchivePath returns the sibling archive path for root, timestamp, and kind.
func ArchivePath(root string, ts time.Time, kind Kind) string {
	return filepath.Clean(root) + "." + ts.Format(timestampLayout) + suffix(kind)
}

// Create writes a compressed tar archive for root as a sibling file.
func Create(root string, ts time.Time, kind Kind) (string, error) {
	root = filepath.Clean(root)
	if err := requireDir(root); err != nil {
		return "", err
	}

	dst := ArchivePath(root, ts, kind)
	if _, err := os.Stat(dst); err == nil {
		return "", fmt.Errorf("backup archive %q already exists", dst)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat backup archive %q: %w", dst, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create backup temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := writeTarGzip(tmp, root); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close backup archive: %w", err)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return "", fmt.Errorf("publish backup archive: %w", err)
	}
	removeTmp = false
	return dst, nil
}

// List returns timestamped sibling archives for root and kind, newest first.
func List(root string, kind Kind) ([]Archive, error) {
	root = filepath.Clean(root)
	parent := filepath.Dir(root)
	base := filepath.Base(root)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, fmt.Errorf("list backup archives: %w", err)
	}

	prefix := base + "."
	suf := suffix(kind)
	var archives []Archive
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suf) {
			continue
		}
		rawTS := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suf)
		ts, err := time.Parse(timestampLayout, rawTS)
		if err != nil {
			continue
		}
		archives = append(archives, Archive{
			Path:      filepath.Join(parent, name),
			Timestamp: ts,
		})
	}
	sort.Slice(archives, func(i, j int) bool {
		return archives[i].Timestamp.After(archives[j].Timestamp)
	})
	return archives, nil
}

// Latest returns the newest normal backup archive for root.
func Latest(root string) (Archive, error) {
	archives, err := List(root, KindNormal)
	if err != nil {
		return Archive{}, err
	}
	if len(archives) == 0 {
		return Archive{}, fmt.Errorf("no backup archives found for %q", filepath.Clean(root))
	}
	return archives[0], nil
}

// Prune removes older sibling archives, keeping the newest keep entries.
func Prune(root string, kind Kind, keep int) (int, error) {
	if keep < 0 {
		return 0, fmt.Errorf("keep must be >= 0")
	}
	archives, err := List(root, kind)
	if err != nil {
		return 0, err
	}
	if len(archives) <= keep {
		return 0, nil
	}
	removed := 0
	for _, archive := range archives[keep:] {
		if err := os.Remove(archive.Path); err != nil {
			return removed, fmt.Errorf("remove old backup archive %q: %w", archive.Path, err)
		}
		removed++
	}
	return removed, nil
}

// DeleteContents removes entries inside root without deleting root itself.
func DeleteContents(root string) error {
	root = filepath.Clean(root)
	if err := requireDir(root); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read backup root: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return fmt.Errorf("delete backup root contents: %w", err)
		}
	}
	return nil
}

// ReplaceContents deletes root contents and extracts archive into root.
func ReplaceContents(root, archivePath string) error {
	if err := DeleteContents(root); err != nil {
		return err
	}
	return RestoreInto(root, archivePath)
}

// RestoreInto extracts archivePath into root after validating all archive paths.
func RestoreInto(root, archivePath string) error {
	root = filepath.Clean(root)
	if err := requireDir(root); err != nil {
		return err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open backup archive: %w", err)
	}
	defer func() { _ = file.Close() }()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read backup gzip: %w", err)
	}
	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read backup tar: %w", err)
		}
		if err := restoreEntry(root, header, tr); err != nil {
			return err
		}
	}
}

func writeTarGzip(dst io.Writer, root string) error {
	gzw := gzip.NewWriter(dst)
	tw := tar.NewWriter(gzw)

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if isMetadataArtifact(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, file); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
		return nil
	})
	if walkErr != nil {
		_ = tw.Close()
		_ = gzw.Close()
		return fmt.Errorf("write backup archive: %w", walkErr)
	}
	if err := tw.Close(); err != nil {
		_ = gzw.Close()
		return fmt.Errorf("close backup tar: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return fmt.Errorf("close backup gzip: %w", err)
	}
	return nil
}

func restoreEntry(root string, header *tar.Header, tr *tar.Reader) error {
	dest, err := safeDestination(root, header.Name)
	if err != nil {
		return err
	}

	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(dest, fs.FileMode(header.Mode)&0o777)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("create backup restore parent: %w", err)
		}
		file, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.FileMode(header.Mode)&0o777)
		if err != nil {
			return fmt.Errorf("create backup restore file: %w", err)
		}
		if _, err := io.Copy(file, tr); err != nil {
			_ = file.Close()
			return fmt.Errorf("write backup restore file: %w", err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close backup restore file: %w", err)
		}
		return nil
	case tar.TypeSymlink:
		if err := validateSymlinkTarget(root, dest, header.Linkname); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("create backup restore parent: %w", err)
		}
		_ = os.Remove(dest)
		if err := os.Symlink(header.Linkname, dest); err != nil {
			return fmt.Errorf("create backup restore symlink: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported backup archive entry %q type %d", header.Name, header.Typeflag)
	}
}

func safeDestination(root, name string) (string, error) {
	if name == "" || filepath.IsAbs(name) {
		return "", fmt.Errorf("unsafe backup archive path %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("unsafe backup archive path %q", name)
	}
	dest := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, dest)
	if err != nil {
		return "", fmt.Errorf("validate backup archive path %q: %w", name, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("unsafe backup archive path %q", name)
	}
	return dest, nil
}

func validateSymlinkTarget(root, linkPath, target string) error {
	if target == "" || filepath.IsAbs(target) {
		return fmt.Errorf("unsafe backup symlink target %q", target)
	}
	targetPath := filepath.Clean(filepath.Join(filepath.Dir(linkPath), target))
	rel, err := filepath.Rel(root, targetPath)
	if err != nil {
		return fmt.Errorf("validate backup symlink target %q: %w", target, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("unsafe backup symlink target %q", target)
	}
	return nil
}

func requireDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("backup root %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("backup root %q is not a directory", path)
	}
	return nil
}

func suffix(kind Kind) string {
	if kind == KindPreRestore {
		return ".pre-restore.backup.tar.gz"
	}
	return ".backup.tar.gz"
}

func isMetadataArtifact(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}
