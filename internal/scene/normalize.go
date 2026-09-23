package scene

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

var supportedTypes = map[string]bool{
	"rectangle": true, "diamond": true, "ellipse": true, "text": true,
	"line": true, "arrow": true, "freedraw": true, "frame": true,
	"magicframe": true, "embeddable": true, "image": true,
}

func normalizeScene(in Scene) (Scene, error) {
	out := cloneScene(in)
	out.Type = "excalidraw"
	out.Version = 2
	if out.Source == "" {
		out.Source = "https://github.com/hangarbay/excalidraw.mcp"
	}
	if out.Elements == nil {
		out.Elements = []Element{}
	}
	if out.AppState == nil {
		out.AppState = map[string]any{}
	}
	if out.Files == nil {
		out.Files = map[string]any{}
	}
	seen := make(map[string]struct{}, len(out.Elements))
	for i, element := range out.Elements {
		normalized, err := normalizeElement(element)
		if err != nil {
			return Scene{}, fmt.Errorf("element %d: %w", i, err)
		}
		id := normalized["id"].(string)
		if _, exists := seen[id]; exists {
			return Scene{}, fmt.Errorf("element %d: duplicate id %q", i, id)
		}
		seen[id] = struct{}{}
		out.Elements[i] = normalized
	}
	return out, nil
}

func normalizeElement(in Element) (Element, error) {
	if in == nil {
		return nil, errors.New("element must be an object")
	}
	out := cloneMap(in)
	typeName, _ := out["type"].(string)
	if !supportedTypes[typeName] {
		return nil, fmt.Errorf("unsupported type %q", typeName)
	}
	id, _ := out["id"].(string)
	if strings.TrimSpace(id) == "" {
		id = randomID()
		out["id"] = id
	}

	setDefault(out, "x", float64(0))
	setDefault(out, "y", float64(0))
	setDefault(out, "angle", float64(0))
	setDefault(out, "strokeColor", "#1b1b1f")
	setDefault(out, "backgroundColor", "transparent")
	setDefault(out, "fillStyle", "solid")
	setDefault(out, "strokeWidth", float64(2))
	setDefault(out, "strokeStyle", "solid")
	setDefault(out, "roughness", float64(1))
	setDefault(out, "opacity", float64(100))
	setDefault(out, "groupIds", []any{})
	setDefault(out, "frameId", nil)
	setDefault(out, "roundness", nil)
	setDefault(out, "seed", float64(randomPositiveInt()))
	setDefault(out, "version", float64(1))
	setDefault(out, "versionNonce", float64(randomPositiveInt()))
	setDefault(out, "isDeleted", false)
	setDefault(out, "boundElements", nil)
	setDefault(out, "updated", float64(time.Now().UnixMilli()))
	setDefault(out, "link", nil)
	setDefault(out, "locked", false)

	switch typeName {
	case "text":
		normalizeText(out)
	case "line", "arrow":
		normalizeLinear(out, typeName)
	case "freedraw":
		normalizeFreedraw(out)
	default:
		setDefault(out, "width", float64(160))
		setDefault(out, "height", float64(100))
	}
	return out, nil
}

func normalizeText(out Element) {
	text, _ := out["text"].(string)
	setDefault(out, "text", text)
	setDefault(out, "originalText", text)
	setDefault(out, "fontSize", float64(20))
	setDefault(out, "fontFamily", float64(5))
	setDefault(out, "textAlign", "left")
	setDefault(out, "verticalAlign", "top")
	setDefault(out, "containerId", nil)
	setDefault(out, "autoResize", true)
	setDefault(out, "lineHeight", float64(1.25))
	fontSize := number(out["fontSize"], 20)
	lines := strings.Split(text, "\n")
	maxRunes := 1
	for _, line := range lines {
		maxRunes = max(maxRunes, utf8.RuneCountInString(line))
	}
	setDefault(out, "width", math.Max(20, float64(maxRunes)*fontSize*0.6))
	setDefault(out, "height", math.Max(fontSize*1.25, float64(len(lines))*fontSize*1.25))
}

func normalizeLinear(out Element, typeName string) {
	setDefault(out, "points", []any{[]any{float64(0), float64(0)}, []any{float64(120), float64(0)}})
	setDefault(out, "lastCommittedPoint", nil)
	setDefault(out, "startBinding", nil)
	setDefault(out, "endBinding", nil)
	setDefault(out, "startArrowhead", nil)
	if typeName == "arrow" {
		setDefault(out, "endArrowhead", "arrow")
	} else {
		setDefault(out, "endArrowhead", nil)
	}
	setDefault(out, "elbowed", false)
	setDefault(out, "width", float64(120))
	setDefault(out, "height", float64(0))
}

func normalizeFreedraw(out Element) {
	setDefault(out, "points", []any{[]any{float64(0), float64(0)}, []any{float64(20), float64(20)}})
	setDefault(out, "pressures", []any{})
	setDefault(out, "simulatePressure", true)
	setDefault(out, "lastCommittedPoint", nil)
	setDefault(out, "width", float64(20))
	setDefault(out, "height", float64(20))
}

func setDefault(m map[string]any, key string, value any) {
	if _, exists := m[key]; !exists {
		m[key] = value
	}
}

func number(value any, fallback float64) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case uint64:
		return float64(v)
	default:
		return fallback
	}
}

func randomID() string {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprintf("element-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data[:])
}

func randomPositiveInt() int64 {
	var data [4]byte
	if _, err := rand.Read(data[:]); err != nil {
		return time.Now().UnixNano() & math.MaxInt32
	}
	return int64(binary.LittleEndian.Uint32(data[:])&math.MaxInt32) + 1
}
