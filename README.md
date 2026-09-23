# excalidraw.mcp

A local-first MCP server for programmatic Excalidraw. One Go process exposes scene tools over MCP stdio or Streamable HTTP and serves the official Excalidraw editor in a browser. MCP and browser edits share the same revisioned scene in real time.

## Features

- Official `@excalidraw/excalidraw` editor, compiled and embedded in the Go binary
- Seven typed MCP tools for reading, loading, and mutating scenes
- Live MCP-to-browser updates over server-sent events
- Debounced browser-to-server updates with optimistic revision checks
- Portable `.excalidraw` persistence with atomic writes
- No external service, account, or API key

## Build and run

Requirements: Go 1.27.1 and Node.js 26 or newer.

```sh
make build
./excalidraw-mcp serve
```

The MCP transport uses stdin/stdout. The browser editor defaults to <http://127.0.0.1:3210>. Startup details are written to stderr so they do not corrupt MCP traffic.

Run only the browser editor:

```sh
./excalidraw-mcp web
```

Useful options:

```text
-listen 127.0.0.1:3210       HTTP listen address
-public-url https://draw.example.com
-scene /path/to/file.excalidraw
-scene -                     disable persistence
```

`EXCALIDRAW_MCP_LISTEN`, `EXCALIDRAW_MCP_PUBLIC_URL`, and `EXCALIDRAW_MCP_SCENE` set the corresponding defaults. Without an explicit scene path, `serve` and `web` store `excalidraw.mcp/scene.excalidraw` below the operating system user config directory. The continuous `http` command defaults to in-memory state.

## Always-on HTTP service

Run one long-lived process for both MCP clients and the browser:

```sh
./excalidraw-mcp http
```

- MCP endpoint: <http://127.0.0.1:3210/mcp>
- Browser editor: <http://127.0.0.1:3210/>

An MCP client can send the complete UTF-8 contents of any `.excalidraw` file to `open_scene`. The shared browser canvas updates immediately; the service does not need access to the agent's filesystem.

For Docker:

```sh
docker build -t excalidraw.mcp:local .
docker run --rm -p 127.0.0.1:3210:3210 excalidraw.mcp:local
```

The image starts the continuous HTTP service with in-memory scene state. No scene-file argument or volume mount is required. Set `EXCALIDRAW_MCP_PUBLIC_URL` when clients must receive a URL other than `http://localhost:3210`.

## MCP client configuration

For an always-on deployment, configure the MCP client with the Streamable HTTP endpoint (the exact surrounding client schema may vary):

```json
{
  "mcpServers": {
    "excalidraw": {
      "url": "http://127.0.0.1:3210/mcp"
    }
  }
}
```

For a client-managed local process, use stdio:

```json
{
  "mcpServers": {
    "excalidraw": {
      "command": "/absolute/path/to/excalidraw-mcp",
      "args": ["serve"]
    }
  }
}
```

## Tools

| Tool | Behavior |
| --- | --- |
| `open_scene` | Load and visualize the complete contents of a `.excalidraw` file sent by the client. |
| `get_scene` | Return the complete scene, current revision, and browser URL. |
| `replace_scene` | Replace the complete Excalidraw document. |
| `add_elements` | Append elements and generate omitted Excalidraw metadata. |
| `update_elements` | Merge field patches into elements by ID. |
| `delete_elements` | Atomically remove elements by ID. |
| `clear_scene` | Remove all elements and binary files. |

Every mutation accepts an optional `expectedRevision`. When supplied, a concurrent browser or MCP edit causes the mutation to fail instead of overwriting newer work.

Minimal `open_scene` arguments:

```json
{
  "name": "system-architecture",
  "content": "{\"type\":\"excalidraw\",\"version\":2,\"elements\":[],\"appState\":{},\"files\":{}}"
}
```

Minimal `add_elements` arguments:

```json
{
  "elements": [
    {
      "type": "rectangle",
      "x": 100,
      "y": 100,
      "width": 280,
      "height": 120,
      "backgroundColor": "#d0ebff"
    },
    {
      "type": "text",
      "x": 145,
      "y": 145,
      "text": "Built through MCP",
      "fontSize": 28
    }
  ]
}
```

Supported normalized types are `rectangle`, `diamond`, `ellipse`, `text`, `line`, `arrow`, `freedraw`, `frame`, `magicframe`, `embeddable`, and `image`. Unknown Excalidraw fields are preserved, allowing complete scenes from the browser or `.excalidraw` files to round-trip through the server.

## Local HTTP API

The embedded editor uses these endpoints:

- `POST /mcp` — Streamable HTTP MCP endpoint in `http` mode
- `GET /api/scene` — current `{revision, scene}` snapshot
- `PUT /api/scene` — replace a scene with optional `expectedRevision` and `clientId`
- `GET /api/events` — live scene event stream
- `GET /healthz` — health check

The HTTP service has no authentication. Bind and publish it to loopback for local use. Put a trusted authentication reverse proxy in front of any remote deployment.

## Development

```sh
make web    # install browser dependencies and compile embedded assets
make test   # rebuild browser assets, vet, and run Go tests
make build
make serve  # build, then run MCP over stdio and the browser editor
make docker
```

The Vite build is emitted to `internal/webui/dist` and embedded with `go:embed`. `go build` therefore produces one self-contained executable.
