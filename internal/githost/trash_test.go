package githost

import (
	"os"
	"testing"
	"time"
)

func TestTrashRoundTrip(t *testing.T) {
	h := newHost(t)
	fixedClock(h)
	mustCreate(t, h, "notes")
	id, err := h.Delete(ctx, "notes")
	if err != nil || id != "notes-20261107-120000" {
		t.Fatalf("Delete: %q %v", id, err)
	}
	if list, _ := h.List(ctx); len(list) != 0 {
		t.Errorf("still listed: %+v", list)
	}
	mustCreate(t, h, "notes")
	id2, err := h.Delete(ctx, "notes")
	if err != nil || id2 != "notes-20261107-120000-2" {
		t.Fatalf("second Delete: %q %v", id2, err)
	}
	trash, err := h.ListTrash()
	if err != nil || len(trash) != 2 {
		t.Fatalf("ListTrash: %+v %v", trash, err)
	}
	if trash[0].Name != "notes" || !trash[0].DeletedAt.Equal(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)) || trash[0].Size == 0 {
		t.Errorf("trash entry: %+v", trash[0])
	}

	name, err := h.Restore(ctx, id)
	if err != nil || name != "notes" {
		t.Fatalf("Restore: %q %v", name, err)
	}
	if !h.Exists("notes") {
		t.Error("not restored")
	}
	if _, err := h.Restore(ctx, id2); !isKind(err, Exists) {
		t.Errorf("restore over a taken name: %v", err)
	}
	if _, err := h.Restore(ctx, "nope-20261107-120000"); !isKind(err, NotFound) {
		t.Errorf("restore missing: %v", err)
	}
	if _, err := h.Restore(ctx, "../x"); !isKind(err, Invalid) {
		t.Errorf("restore bad id: %v", err)
	}
	err = h.Purge(ctx, id2, "wrong")
	if !isKind(err, Invalid) || err.Error() != "Type the repository's name to delete it for good" {
		t.Errorf("purge without the name: %v", err)
	}
	if err := h.Purge(ctx, id2, "notes"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.trashPath(id2)); !os.IsNotExist(err) {
		t.Errorf("purged folder still there: %v", err)
	}
	if _, err := h.Delete(ctx, "nope"); !isKind(err, NotFound) {
		t.Errorf("delete missing: %v", err)
	}
}
