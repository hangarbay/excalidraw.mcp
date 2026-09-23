package scene

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrRevisionConflict = errors.New("scene revision conflict")

type Store struct {
	mu          sync.RWMutex
	path        string
	revision    uint64
	scene       Scene
	nextSubID   uint64
	subscribers map[uint64]chan Event
}

func NewStore(path string) (*Store, error) {
	initial, err := normalizeScene(Scene{})
	if err != nil {
		return nil, err
	}
	s := &Store{path: path, scene: initial, subscribers: map[uint64]chan Event{}}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read scene: %w", err)
	}
	var loaded Scene
	if err := json.Unmarshal(data, &loaded); err != nil {
		return nil, fmt.Errorf("decode scene: %w", err)
	}
	loaded, err = normalizeScene(loaded)
	if err != nil {
		return nil, fmt.Errorf("normalize scene: %w", err)
	}
	s.scene = loaded
	return s, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Snapshot{Revision: s.revision, Scene: cloneScene(s.scene)}
}

func (s *Store) Replace(next Scene, expected *uint64, source string) (Snapshot, error) {
	return s.mutate(expected, source, func(_ Scene) (Scene, error) {
		return normalizeScene(next)
	})
}

func (s *Store) Add(elements []Element, expected *uint64, source string) (Snapshot, error) {
	return s.mutate(expected, source, func(next Scene) (Scene, error) {
		existing := make(map[string]struct{}, len(next.Elements))
		for _, element := range next.Elements {
			if id, ok := element["id"].(string); ok {
				existing[id] = struct{}{}
			}
		}
		for i, element := range elements {
			normalized, err := normalizeElement(element)
			if err != nil {
				return Scene{}, fmt.Errorf("element %d: %w", i, err)
			}
			id := normalized["id"].(string)
			if _, exists := existing[id]; exists {
				return Scene{}, fmt.Errorf("element %d: duplicate id %q", i, id)
			}
			existing[id] = struct{}{}
			next.Elements = append(next.Elements, normalized)
		}
		return next, nil
	})
}

func (s *Store) Update(patches []Patch, expected *uint64, source string) (Snapshot, error) {
	return s.mutate(expected, source, func(next Scene) (Scene, error) {
		byID := make(map[string]int, len(next.Elements))
		for i, element := range next.Elements {
			if id, ok := element["id"].(string); ok {
				byID[id] = i
			}
		}
		for i, patch := range patches {
			index, exists := byID[patch.ID]
			if !exists {
				return Scene{}, fmt.Errorf("patch %d: element %q not found", i, patch.ID)
			}
			updated := cloneMap(next.Elements[index])
			for key, value := range patch.Changes {
				if key != "id" {
					updated[key] = cloneValue(value)
				}
			}
			updated["version"] = number(updated["version"], 1) + 1
			updated["versionNonce"] = float64(randomPositiveInt())
			normalized, err := normalizeElement(updated)
			if err != nil {
				return Scene{}, fmt.Errorf("patch %d: %w", i, err)
			}
			next.Elements[index] = normalized
		}
		return next, nil
	})
}

func (s *Store) Delete(ids []string, expected *uint64, source string) (Snapshot, error) {
	return s.mutate(expected, source, func(next Scene) (Scene, error) {
		remove := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			remove[id] = struct{}{}
		}
		kept := make([]Element, 0, len(next.Elements))
		found := 0
		for _, element := range next.Elements {
			id, _ := element["id"].(string)
			if _, deleted := remove[id]; deleted {
				found++
				continue
			}
			kept = append(kept, element)
		}
		if found != len(remove) {
			return Scene{}, fmt.Errorf("one or more element IDs were not found")
		}
		next.Elements = kept
		return next, nil
	})
}

func (s *Store) Clear(expected *uint64, source string) (Snapshot, error) {
	return s.mutate(expected, source, func(next Scene) (Scene, error) {
		next.Elements = []Element{}
		next.Files = map[string]any{}
		return next, nil
	})
}

func (s *Store) Subscribe() (<-chan Event, func()) {
	s.mu.Lock()
	id := s.nextSubID
	s.nextSubID++
	ch := make(chan Event, 8)
	s.subscribers[id] = ch
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subscribers, id)
		s.mu.Unlock()
	}
}

func (s *Store) mutate(expected *uint64, source string, change func(Scene) (Scene, error)) (Snapshot, error) {
	s.mu.Lock()
	if expected != nil && *expected != s.revision {
		actual := s.revision
		s.mu.Unlock()
		return Snapshot{}, fmt.Errorf("%w: expected %d, current %d", ErrRevisionConflict, *expected, actual)
	}
	next, err := change(cloneScene(s.scene))
	if err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	next, err = normalizeScene(next)
	if err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	if err := persist(s.path, next); err != nil {
		s.mu.Unlock()
		return Snapshot{}, err
	}
	s.revision++
	s.scene = next
	snapshot := Snapshot{Revision: s.revision, Scene: cloneScene(next)}
	subscribers := make([]chan Event, 0, len(s.subscribers))
	for _, ch := range s.subscribers {
		subscribers = append(subscribers, ch)
	}
	s.mu.Unlock()

	event := Event{Snapshot: snapshot, Source: source}
	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
		}
	}
	return snapshot, nil
}

func persist(path string, scene Scene) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create scene directory: %w", err)
	}
	data, err := json.MarshalIndent(scene, "", "  ")
	if err != nil {
		return fmt.Errorf("encode scene: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".scene-*")
	if err != nil {
		return fmt.Errorf("create temporary scene: %w", err)
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write scene: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync scene: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close scene: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace scene: %w", err)
	}
	return nil
}

func cloneScene(in Scene) Scene {
	out := in
	out.Elements = make([]Element, len(in.Elements))
	for i, element := range in.Elements {
		out.Elements[i] = cloneMap(element)
	}
	out.AppState = cloneMap(in.AppState)
	out.Files = cloneMap(in.Files)
	return out
}

func cloneMap[M ~map[string]any](in M) M {
	if in == nil {
		return nil
	}
	out := make(M, len(in))
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneMap(value)
	case Element:
		return cloneMap(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = cloneValue(item)
		}
		return out
	case []Element:
		out := make([]Element, len(value))
		for i, item := range value {
			out[i] = cloneMap(item)
		}
		return out
	default:
		return value
	}
}
