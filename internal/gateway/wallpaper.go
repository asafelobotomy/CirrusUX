package gateway

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// The wallpaper image (decision D7) is stored on its own, not inside the UI
// settings: those are saved on every change, and an inline 5 MB image made
// each save, and each of the 20 kept versions, that much bigger.

const maxWallpaper = 5 << 20

// Raster formats only: an SVG can carry script.
var wallpaperTypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif", "image/avif"}

type wallpaperMeta struct {
	Type    string    `json:"type"`
	SavedAt time.Time `json:"savedAt"`
}

// sniffImage reports the image type from the bytes themselves, whatever the
// browser claimed.
func sniffImage(b []byte) string {
	// Go's sniffer doesn't know AVIF: an ISO-BMFF file with an avif brand.
	if len(b) >= 12 && (bytes.Equal(b[4:12], []byte("ftypavif")) || bytes.Equal(b[4:12], []byte("ftypavis"))) {
		return "image/avif"
	}
	return http.DetectContentType(b)
}

func (s *Server) putWallpaper(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWallpaper))
	if err != nil {
		http.Error(w, "images up to 5 MB", http.StatusRequestEntityTooLarge)
		return
	}
	typ := sniffImage(b)
	if !slices.Contains(wallpaperTypes, typ) {
		http.Error(w, "not a PNG, JPEG, WebP, GIF or AVIF image", http.StatusUnsupportedMediaType)
		return
	}
	meta, err := s.store.SaveWallpaper(b, typ)
	if err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"savedAt": meta.SavedAt.UnixMilli()})
}

func (s *Server) getWallpaper(w http.ResponseWriter, r *http.Request) {
	b, meta, err := s.store.Wallpaper()
	if err != nil {
		http.Error(w, "no wallpaper", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", meta.Type)
	// The URL carries ?v=<savedAt>, so a new image gets a new URL.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(b)
}

func (s *Store) SaveWallpaper(b []byte, typ string) (wallpaperMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta := wallpaperMeta{Type: typ, SavedAt: time.Now()}
	if err := writeFileAtomic(filepath.Join(s.dir, "wallpaper.bin"), b); err != nil {
		return meta, err
	}
	return meta, writeFileJSON(filepath.Join(s.dir, "wallpaper.json"), meta)
}

func (s *Store) Wallpaper() ([]byte, wallpaperMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var meta wallpaperMeta
	if err := readFileJSON(filepath.Join(s.dir, "wallpaper.json"), &meta); err != nil {
		return nil, meta, err
	}
	// Only a type this server accepted is ever served.
	if !slices.Contains(wallpaperTypes, meta.Type) {
		return nil, meta, os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(s.dir, "wallpaper.bin"))
	return b, meta, err
}
