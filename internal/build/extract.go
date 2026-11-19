package build

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Limits on the source the helper unpacks.
const (
	MaxSourceBytes   = 2 << 30 // unpacked
	MaxSourceEntries = 200_000
)

// Extract unpacks a .tar.gz (git archive's output) into dir, which must
// exist and be empty. It runs as root on a file the agent handed over, so
// it trusts nothing in it: every name must stay inside dir, nothing is
// written through a link, only folders, files and links are made, and
// files get plain permissions (no set-user-ID bits).
func Extract(r io.Reader, dir string) error {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("source: %v", err)
	}
	tr := tar.NewReader(zr)
	var total int64
	for n := 0; ; n++ {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("source: %v", err)
		}
		if n >= MaxSourceEntries {
			return fmt.Errorf("source: more than %d files", MaxSourceEntries)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue // git archive's commit ID
		}
		name, err := entryName(h.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		target := filepath.Join(dir, name)
		if err := parentsAreFolders(dir, name); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(target, 0o755); err != nil && !isDir(target) {
				return fmt.Errorf("source: %v", err)
			}
		case tar.TypeReg:
			total += h.Size
			if h.Size < 0 || total > MaxSourceBytes {
				return fmt.Errorf("source: more than %d MB unpacked", MaxSourceBytes>>20)
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				mode = 0o755
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
			if err != nil {
				return fmt.Errorf("source: %v", err)
			}
			_, err = io.CopyN(f, tr, h.Size)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return fmt.Errorf("source: %v", err)
			}
		case tar.TypeSymlink:
			// The link is made, never followed here; inside the build
			// container it points within the container.
			if err := os.Symlink(h.Linkname, target); err != nil {
				return fmt.Errorf("source: %v", err)
			}
		default:
			// Hard links, devices and the like: git archive makes none.
		}
	}
}

// entryName cleans an entry's name, refusing one that leaves the folder.
func entryName(raw string) (string, error) {
	n := strings.TrimPrefix(raw, "./")
	if n == "" || n == "." {
		return "", nil
	}
	if len(n) > 4096 || strings.ContainsRune(n, 0) || filepath.IsAbs(n) {
		return "", fmt.Errorf("source: bad name %q", raw)
	}
	clean := filepath.Clean(n)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("source: %q leaves the folder", raw)
	}
	return clean, nil
}

// parentsAreFolders checks each folder on the way to name is a real
// folder, not a link that would lead out of dir.
func parentsAreFolders(dir, name string) error {
	p := dir
	parts := strings.Split(filepath.Dir(name), string(filepath.Separator))
	for _, part := range parts {
		if part == "." {
			continue
		}
		p = filepath.Join(p, part)
		fi, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Mkdir(p, 0o755); err != nil {
				return fmt.Errorf("source: %v", err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("source: %v", err)
		}
		if !fi.IsDir() {
			return fmt.Errorf("source: %s goes through a link or a file", name)
		}
	}
	return nil
}

func isDir(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.IsDir()
}

// ChownTree gives everything under dir (dir included) to uid:gid, without
// following links.
func ChownTree(dir string, uid, gid int) error {
	return filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, uid, gid)
	})
}
