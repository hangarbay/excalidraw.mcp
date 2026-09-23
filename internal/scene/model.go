package scene

// Element is an Excalidraw element. A map is intentional: Excalidraw adds
// fields between releases and the server must preserve fields it does not yet
// understand.
type Element map[string]any

// Scene is the portable .excalidraw document shape.
type Scene struct {
	Type     string         `json:"type"`
	Version  int            `json:"version"`
	Source   string         `json:"source"`
	Elements []Element      `json:"elements"`
	AppState map[string]any `json:"appState"`
	Files    map[string]any `json:"files"`
}

// Snapshot couples a scene with its server-side revision.
type Snapshot struct {
	Revision uint64 `json:"revision"`
	Scene    Scene  `json:"scene"`
}

// Event identifies the writer of a committed snapshot.
type Event struct {
	Snapshot
	Source string `json:"source"`
}

// Patch updates one element without replacing unspecified fields.
type Patch struct {
	ID      string         `json:"id" jsonschema:"ID of the element to update"`
	Changes map[string]any `json:"changes" jsonschema:"Excalidraw element fields to merge; id cannot be changed"`
}
