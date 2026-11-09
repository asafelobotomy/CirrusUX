package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cirrusux/internal/store"
)

// storeDir is store.SourcesDir; tests swap it.
var storeDir = store.SourcesDir

// storeSources are the CirrusStore sources this server trusts: the
// official one (unless STORE_OFFICIAL=no) and each store.d/<name>.source,
// which must belong to root like the helper's own config. A file that
// fails is reported, not trusted, and doesn't stop the others.
func storeSources(c conf) ([]store.Source, []store.Info) {
	var srcs []store.Source
	var infos []store.Info
	if !c.StoreNoOfficial {
		s := store.OfficialSource()
		srcs = append(srcs, s)
		infos = append(infos, s.Info())
	}
	files, _ := filepath.Glob(filepath.Join(storeDir, "*.source"))
	sort.Strings(files)
	for _, p := range files {
		name := strings.TrimSuffix(filepath.Base(p), ".source")
		s, err := readSource(name, p)
		if err != nil {
			infos = append(infos, store.Info{Name: name, Keys: []string{}, Fingerprints: []string{}, Error: err.Error()})
			continue
		}
		srcs = append(srcs, s)
		infos = append(infos, s.Info())
	}
	return srcs, infos
}

func readSource(name, p string) (store.Source, error) {
	f, err := os.Open(p)
	if err != nil {
		return store.Source{}, err
	}
	defer f.Close()
	if err := ownerCheck(f); err != nil {
		return store.Source{}, err
	}
	b, err := io.ReadAll(io.LimitReader(f, 16<<10))
	if err != nil {
		return store.Source{}, err
	}
	s, err := store.ParseSource(name, b)
	if err != nil {
		return s, fmt.Errorf("%s: %v", p, err)
	}
	return s, nil
}

// findSource is the trusted source named name.
func findSource(srcs []store.Source, name string) (store.Source, bool) {
	for _, s := range srcs {
		if s.Name == name {
			return s, true
		}
	}
	return store.Source{}, false
}
