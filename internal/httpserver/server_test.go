package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hangarbay/excalidraw.mcp/internal/scene"
)

func TestSceneAPIUsesOptimisticRevision(t *testing.T) {
	store, err := scene.NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store).Handler())
	defer server.Close()

	body := map[string]any{
		"expectedRevision": 0,
		"clientId":         "browser-test",
		"scene": map[string]any{
			"type": "excalidraw", "version": 2,
			"elements": []any{map[string]any{"id": "box", "type": "rectangle"}},
			"appState": map[string]any{}, "files": map[string]any{},
		},
	}
	payload, _ := json.Marshal(body)
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/api/scene", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("first PUT status = %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPut, server.URL+"/api/scene", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stale PUT status = %d, want 409", response.StatusCode)
	}
	var current scene.Snapshot
	if err := json.NewDecoder(response.Body).Decode(&current); err != nil {
		t.Fatal(err)
	}
	if current.Revision != 1 || len(current.Scene.Elements) != 1 {
		t.Fatalf("conflict snapshot = %#v", current)
	}
}

func TestEmbeddedEditorIsServed(t *testing.T) {
	store, _ := scene.NewStore("")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	New(store).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET / status = %d", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
}
