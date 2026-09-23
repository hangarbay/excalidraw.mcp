package scene

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStorePersistsMutationsAndRejectsStaleRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drawing.excalidraw")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := store.Subscribe()
	defer unsubscribe()

	initial := uint64(0)
	added, err := store.Add([]Element{{
		"type": "rectangle", "x": 10.0, "y": 20.0, "width": 80.0, "height": 40.0,
	}}, &initial, "test")
	if err != nil {
		t.Fatalf("Add() = %v", err)
	}
	if added.Revision != 1 || len(added.Scene.Elements) != 1 {
		t.Fatalf("Add() snapshot = %#v", added)
	}
	id, _ := added.Scene.Elements[0]["id"].(string)
	if id == "" {
		t.Fatal("normalized element has no ID")
	}
	if event := <-events; event.Source != "test" || event.Revision != 1 {
		t.Fatalf("event = %#v", event)
	}

	_, err = store.Update([]Patch{{ID: id, Changes: map[string]any{"backgroundColor": "#ffc9c9"}}}, &initial, "test")
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale Update() error = %v, want ErrRevisionConflict", err)
	}
	current := uint64(1)
	updated, err := store.Update([]Patch{{ID: id, Changes: map[string]any{"backgroundColor": "#ffc9c9"}}}, &current, "test")
	if err != nil {
		t.Fatalf("Update() = %v", err)
	}
	if got := updated.Scene.Elements[0]["backgroundColor"]; got != "#ffc9c9" {
		t.Fatalf("backgroundColor = %v", got)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("persistent scene: %v", err)
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore(persisted) = %v", err)
	}
	loaded := reloaded.Snapshot()
	if len(loaded.Scene.Elements) != 1 || loaded.Scene.Elements[0]["id"] != id {
		t.Fatalf("reloaded scene = %#v", loaded.Scene)
	}
}

func TestDeleteIsAtomicWhenAnIDIsMissing(t *testing.T) {
	store, err := NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	added, err := store.Add([]Element{{"id": "kept", "type": "ellipse"}}, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Delete([]string{"kept", "missing"}, &added.Revision, "test")
	if err == nil {
		t.Fatal("Delete() succeeded with a missing ID")
	}
	if got := len(store.Snapshot().Scene.Elements); got != 1 {
		t.Fatalf("element count after failed delete = %d, want 1", got)
	}
}
