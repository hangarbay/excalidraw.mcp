// Package tools exposes the shared Excalidraw scene as MCP tools.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hangarbay/excalidraw.mcp/internal/scene"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mutationOptions struct {
	ExpectedRevision *uint64 `json:"expectedRevision,omitempty" jsonschema:"optional revision guard; the mutation fails if the scene changed"`
}

type openSceneInput struct {
	Content          string  `json:"content" jsonschema:"complete contents of a .excalidraw JSON file"`
	Name             string  `json:"name,omitempty" jsonschema:"optional display name for the loaded scene"`
	ExpectedRevision *uint64 `json:"expectedRevision,omitempty" jsonschema:"optional revision guard; the mutation fails if the scene changed"`
}
type replaceInput struct {
	Scene            scene.Scene `json:"scene" jsonschema:"complete Excalidraw scene to store"`
	ExpectedRevision *uint64     `json:"expectedRevision,omitempty" jsonschema:"optional revision guard; the mutation fails if the scene changed"`
}

type addInput struct {
	Elements         []scene.Element `json:"elements" jsonschema:"Excalidraw elements to append; type is required and omitted standard fields are generated"`
	ExpectedRevision *uint64         `json:"expectedRevision,omitempty" jsonschema:"optional revision guard; the mutation fails if the scene changed"`
}

type updateInput struct {
	Patches          []scene.Patch `json:"patches" jsonschema:"element patches to apply in order"`
	ExpectedRevision *uint64       `json:"expectedRevision,omitempty" jsonschema:"optional revision guard; the mutation fails if the scene changed"`
}

type deleteInput struct {
	IDs              []string `json:"ids" jsonschema:"IDs of elements to remove"`
	ExpectedRevision *uint64  `json:"expectedRevision,omitempty" jsonschema:"optional revision guard; the mutation fails if the scene changed"`
}

type Output struct {
	Revision     uint64       `json:"revision"`
	ElementCount int          `json:"elementCount"`
	BrowserURL   string       `json:"browserUrl"`
	Scene        *scene.Scene `json:"scene,omitempty"`
}

type Service struct {
	store      *scene.Store
	browserURL string
}

func Register(server *mcp.Server, store *scene.Store, browserURL string) {
	s := &Service{store: store, browserURL: browserURL}
	closed := false
	destructive := true
	additive := false

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_scene",
		Title:       "Get Excalidraw scene",
		Description: "Return the complete current Excalidraw scene, its revision, and the local browser editor URL. Use element IDs from this result for later updates or deletes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closed},
	}, s.get)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "open_scene",
		Title:       "Open Excalidraw scene",
		Description: "Load and visualize the complete contents of a .excalidraw JSON file sent by the client. This replaces the shared scene and returns the browser editor URL; no filesystem mount is required.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &closed, IdempotentHint: true},
	}, s.open)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "replace_scene",
		Title:       "Replace Excalidraw scene",
		Description: "Replace the complete scene with an Excalidraw document. Standard element fields are filled when omitted. Pass expectedRevision to prevent overwriting concurrent browser edits.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &closed, IdempotentHint: true},
	}, s.replace)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_elements",
		Title:       "Add Excalidraw elements",
		Description: "Append rectangle, diamond, ellipse, text, line, arrow, freedraw, frame, magicframe, embeddable, or image elements. Only type is required; IDs and standard Excalidraw fields are generated.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &additive, OpenWorldHint: &closed},
	}, s.add)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_elements",
		Title:       "Update Excalidraw elements",
		Description: "Merge field changes into existing elements by ID. Element versions are advanced automatically. Use get_scene first when editing a scene that may be open in the browser.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &closed},
	}, s.update)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_elements",
		Title:       "Delete Excalidraw elements",
		Description: "Remove elements by ID from the scene. The call is atomic and fails if any requested ID does not exist.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &closed, IdempotentHint: false},
	}, s.delete)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "clear_scene",
		Title:       "Clear Excalidraw scene",
		Description: "Remove every element and binary file from the scene while preserving app state.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &closed, IdempotentHint: true},
	}, s.clear)
}

func (s *Service) get(context.Context, *mcp.CallToolRequest, any) (*mcp.CallToolResult, Output, error) {
	snapshot := s.store.Snapshot()
	return result(snapshot, s.browserURL, true, "Scene contains %d elements at revision %d. Browser editor: %s")
}

func (s *Service) open(_ context.Context, _ *mcp.CallToolRequest, input openSceneInput) (*mcp.CallToolResult, Output, error) {
	if strings.TrimSpace(input.Content) == "" {
		return nil, Output{}, fmt.Errorf("content is required")
	}
	if len(input.Content) > 32<<20 {
		return nil, Output{}, fmt.Errorf("scene file exceeds the 32 MiB limit")
	}
	var document scene.Scene
	if err := json.Unmarshal([]byte(input.Content), &document); err != nil {
		return nil, Output{}, fmt.Errorf("decode .excalidraw file: %w", err)
	}
	if input.Name != "" {
		if document.AppState == nil {
			document.AppState = map[string]any{}
		}
		document.AppState["name"] = input.Name
	}
	snapshot, err := s.store.Replace(document, input.ExpectedRevision, "mcp")
	if err != nil {
		return nil, Output{}, err
	}
	return result(snapshot, s.browserURL, false, "Loaded scene with %d elements at revision %d. Browser editor: %s")
}

func (s *Service) replace(_ context.Context, _ *mcp.CallToolRequest, input replaceInput) (*mcp.CallToolResult, Output, error) {
	snapshot, err := s.store.Replace(input.Scene, input.ExpectedRevision, "mcp")
	if err != nil {
		return nil, Output{}, err
	}
	return result(snapshot, s.browserURL, false, "Replaced scene with %d elements at revision %d. Browser editor: %s")
}

func (s *Service) add(_ context.Context, _ *mcp.CallToolRequest, input addInput) (*mcp.CallToolResult, Output, error) {
	if len(input.Elements) == 0 {
		return nil, Output{}, fmt.Errorf("elements must not be empty")
	}
	snapshot, err := s.store.Add(input.Elements, input.ExpectedRevision, "mcp")
	if err != nil {
		return nil, Output{}, err
	}
	return result(snapshot, s.browserURL, false, "Scene now contains %d elements at revision %d. Browser editor: %s")
}

func (s *Service) update(_ context.Context, _ *mcp.CallToolRequest, input updateInput) (*mcp.CallToolResult, Output, error) {
	if len(input.Patches) == 0 {
		return nil, Output{}, fmt.Errorf("patches must not be empty")
	}
	snapshot, err := s.store.Update(input.Patches, input.ExpectedRevision, "mcp")
	if err != nil {
		return nil, Output{}, err
	}
	return result(snapshot, s.browserURL, false, "Updated scene with %d elements at revision %d. Browser editor: %s")
}

func (s *Service) delete(_ context.Context, _ *mcp.CallToolRequest, input deleteInput) (*mcp.CallToolResult, Output, error) {
	if len(input.IDs) == 0 {
		return nil, Output{}, fmt.Errorf("ids must not be empty")
	}
	snapshot, err := s.store.Delete(input.IDs, input.ExpectedRevision, "mcp")
	if err != nil {
		return nil, Output{}, err
	}
	return result(snapshot, s.browserURL, false, "Scene now contains %d elements at revision %d. Browser editor: %s")
}

func (s *Service) clear(_ context.Context, _ *mcp.CallToolRequest, input mutationOptions) (*mcp.CallToolResult, Output, error) {
	snapshot, err := s.store.Clear(input.ExpectedRevision, "mcp")
	if err != nil {
		return nil, Output{}, err
	}
	return result(snapshot, s.browserURL, false, "Cleared scene; %d elements remain at revision %d. Browser editor: %s")
}

func result(snapshot scene.Snapshot, browserURL string, includeScene bool, format string) (*mcp.CallToolResult, Output, error) {
	count := len(snapshot.Scene.Elements)
	output := Output{Revision: snapshot.Revision, ElementCount: count, BrowserURL: browserURL}
	if includeScene {
		output.Scene = &snapshot.Scene
	}
	text := fmt.Sprintf(format, count, snapshot.Revision, browserURL)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, output, nil
}
