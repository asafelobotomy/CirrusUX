package githost

import (
	"bytes"
	"context"
	"strings"
)

const (
	diffFiles = 300
	diffBytes = 512 << 10
)

// FileDiff is one file's part of a commit.
type FileDiff struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Change  string `json:"change"` // added, deleted, modified, renamed
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Binary  bool   `json:"binary"`
	Patch   string `json:"patch"`
}

// Detail is a commit with its whole message and its changes.
type Detail struct {
	Commit
	Message   string     `json:"message"`
	Files     []FileDiff `json:"files"`
	Truncated bool       `json:"truncated"`
}

// capWriter keeps the first n bytes and quietly drops the rest, so git
// finishes normally however big the diff is.
type capWriter struct {
	buf  bytes.Buffer
	n    int
	over bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.n - w.buf.Len(); len(p) > room {
		w.buf.Write(p[:max(room, 0)])
		w.over = true
		return len(p), nil
	}
	return w.buf.Write(p)
}

// CommitDetail is one commit and its diff, up to 300 files and 512 KiB.
func (h *Host) CommitDetail(ctx context.Context, name, sha string) (Detail, error) {
	if err := validCommit(sha); err != nil {
		return Detail{}, err
	}
	full, _, err := h.resolve(ctx, name, sha)
	if err != nil {
		return Detail{}, err
	}
	// Room for the message and diff headers beyond the patch limit.
	w := &capWriter{n: diffBytes + 1<<20}
	err = h.exec(ctx, h.path(name), w, nil, "show", "--format="+commitFormat+"%x00%B%x00",
		"--patch", "--find-renames", "--no-color", "--no-ext-diff", "--end-of-options", full)
	if err != nil {
		return Detail{}, err
	}
	f := strings.SplitN(w.buf.String(), "\x00", 8)
	if len(f) < 7 {
		return Detail{}, errf(Failed, "git failed")
	}
	d := Detail{Commit: commitFrom(f[:6]), Message: strings.TrimSpace(f[6]), Files: []FileDiff{}}
	if len(f) == 8 {
		d.Files, d.Truncated = parsePatch([]byte(strings.TrimLeft(f[7], "\n")), diffFiles, diffBytes)
	}
	d.Truncated = d.Truncated || w.over
	return d, nil
}

// parsePatch splits git's patch output into files, keeping up to
// maxFiles files and maxBytes of patch text.
func parsePatch(out []byte, maxFiles, maxBytes int) ([]FileDiff, bool) {
	files := []FileDiff{}
	total, truncated := 0, false
	for _, chunk := range splitDiffs(string(out)) {
		if len(files) == maxFiles {
			truncated = true
			break
		}
		fd := parseFile(chunk)
		if room := maxBytes - total; len(chunk) > room {
			cut := chunk[:max(room, 0)]
			if i := strings.LastIndexByte(cut, '\n'); i >= 0 {
				cut = cut[:i+1]
			}
			fd.Patch, truncated = cut, true
			files = append(files, fd)
			break
		}
		total += len(chunk)
		files = append(files, fd)
	}
	return files, truncated
}

func splitDiffs(s string) []string {
	var chunks []string
	for {
		i := strings.Index(s, "diff --git ")
		if i < 0 {
			return chunks
		}
		s = s[i:]
		next := strings.Index(s[1:], "\ndiff --git ")
		if next < 0 {
			return append(chunks, s)
		}
		chunks = append(chunks, s[:next+2])
		s = s[next+2:]
	}
}

func parseFile(chunk string) FileDiff {
	fd := FileDiff{Change: "modified", Patch: chunk}
	header := strings.TrimPrefix(strings.SplitN(chunk, "\n", 2)[0], "diff --git ")
	if i := strings.LastIndex(header, " b/"); i >= 0 {
		fd.Path = header[i+3:]
	}
	inHunks := false
	for _, line := range strings.Split(chunk, "\n") {
		if inHunks {
			switch {
			case strings.HasPrefix(line, "+"):
				fd.Added++
			case strings.HasPrefix(line, "-"):
				fd.Removed++
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "@@"):
			inHunks = true
		case strings.HasPrefix(line, "new file mode"):
			fd.Change = "added"
		case strings.HasPrefix(line, "deleted file mode"):
			fd.Change = "deleted"
		case strings.HasPrefix(line, "rename from "):
			fd.Change, fd.OldPath = "renamed", strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			fd.Path = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "Binary files "):
			fd.Binary = true
		case strings.HasPrefix(line, "+++ b/"):
			fd.Path = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "--- a/") && fd.Change == "deleted":
			fd.Path = strings.TrimPrefix(line, "--- a/")
		}
	}
	return fd
}
