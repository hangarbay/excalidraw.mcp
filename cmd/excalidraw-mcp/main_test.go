package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/hangarbay/excalidraw.mcp/internal/httpserver"
	"github.com/hangarbay/excalidraw.mcp/internal/scene"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	store, err := scene.NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() {
		done <- newMCPServer(store, "http://127.0.0.1:3210").Run(context.Background(), serverTransport)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("Close() = %v", err)
		}
		if err := <-done; err != nil {
			t.Errorf("server.Run() = %v", err)
		}
	})
	return session
}

func TestServerListsSceneTools(t *testing.T) {
	session := connect(t)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
		if strings.TrimSpace(tool.Description) == "" || tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatalf("incomplete tool definition: %#v", tool)
		}
	}
	sort.Strings(names)
	if got, want := strings.Join(names, ","), "add_elements,clear_scene,delete_elements,get_scene,open_scene,replace_scene,update_elements"; got != want {
		t.Fatalf("tools = %s, want %s", got, want)
	}
}

func TestAddAndGetSceneOverMCP(t *testing.T) {
	session := connect(t)
	added, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "add_elements",
		Arguments: map[string]any{"elements": []any{
			map[string]any{"id": "title", "type": "text", "x": 40, "y": 30, "text": "Hello MCP"},
			map[string]any{"id": "box", "type": "rectangle", "x": 20, "y": 20, "width": 180, "height": 80},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if added.IsError {
		t.Fatalf("add_elements: %s", toolText(added))
	}
	structured := structuredContent(t, added)
	if structured["revision"] != float64(1) || structured["elementCount"] != float64(2) {
		t.Fatalf("add output = %#v", structured)
	}

	got, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_scene"})
	if err != nil {
		t.Fatal(err)
	}
	if got.IsError {
		t.Fatalf("get_scene: %s", toolText(got))
	}
	structured = structuredContent(t, got)
	document, ok := structured["scene"].(map[string]any)
	if !ok {
		t.Fatalf("scene = %#v", structured["scene"])
	}
	elements, _ := document["elements"].([]any)
	if len(elements) != 2 || structured["browserUrl"] != "http://127.0.0.1:3210" {
		t.Fatalf("get output = %#v", structured)
	}
}

func TestOpenCompleteSceneOverStreamableHTTP(t *testing.T) {
	store, err := scene.NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	const browserURL = "http://editor.example"
	server := newMCPServer(store, browserURL)
	app := httptest.NewServer(httpserver.NewWithMCP(store, newMCPHTTPHandler(server)).Handler())
	defer app.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "http-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: app.URL + "/mcp",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	opened, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "open_scene",
		Arguments: map[string]any{
			"name": "system-architecture",
			"content": `{
				"type": "excalidraw",
				"version": 2,
				"source": "uploaded.excalidraw",
				"elements": [
					{"id": "service", "type": "rectangle", "x": 40, "y": 60, "width": 240, "height": 120}
				],
				"appState": {"viewBackgroundColor": "#f8f9fa"},
				"files": {}
			}`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened.IsError {
		t.Fatalf("open_scene: %s", toolText(opened))
	}
	output := structuredContent(t, opened)
	if output["elementCount"] != float64(1) || output["browserUrl"] != browserURL {
		t.Fatalf("open output = %#v", output)
	}

	response, err := http.Get(app.URL + "/api/scene")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/scene status = %d", response.StatusCode)
	}
	var snapshot scene.Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Scene.Elements) != 1 || snapshot.Scene.Elements[0]["id"] != "service" {
		t.Fatalf("browser scene = %#v", snapshot.Scene)
	}
	if snapshot.Scene.AppState["name"] != "system-architecture" {
		t.Fatalf("scene name = %#v", snapshot.Scene.AppState["name"])
	}
}

func structuredContent(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	return output
}

func toolText(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	if content, ok := result.Content[0].(*mcp.TextContent); ok {
		return content.Text
	}
	return ""
}
