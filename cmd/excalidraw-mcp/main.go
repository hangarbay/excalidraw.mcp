// Command excalidraw-mcp serves a shared Excalidraw scene over MCP and HTTP.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hangarbay/excalidraw.mcp/internal/httpserver"
	"github.com/hangarbay/excalidraw.mcp/internal/scene"
	"github.com/hangarbay/excalidraw.mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

type runMode string

const (
	modeStdio runMode = "serve"
	modeHTTP  runMode = "http"
	modeWeb   runMode = "web"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	command := "serve"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	switch command {
	case "serve":
		return serveCommand(args, modeStdio)
	case "http":
		return serveCommand(args, modeHTTP)
	case "web":
		return serveCommand(args, modeWeb)
	case "help", "-h", "--help":
		usage()
		return 0
	case "version", "--version":
		fmt.Println(version)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", command)
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `excalidraw-mcp - programmatic Excalidraw with a local browser editor

Usage:
  excalidraw-mcp serve [-listen ADDRESS] [-scene FILE]  Run MCP over stdio and the browser editor
  excalidraw-mcp http  [-listen ADDRESS]                 Run continuous MCP over HTTP and the browser editor
  excalidraw-mcp web   [-listen ADDRESS] [-scene FILE]  Run only the browser editor
  excalidraw-mcp version                                  Print the version

Options:
  -listen      HTTP listen address (default 127.0.0.1:3210)
  -public-url  externally reachable browser URL returned by MCP tools
  -scene       optional persistent .excalidraw file; use "-" for in-memory state

MCP endpoint in http mode: /mcp
MCP tools: open_scene, get_scene, replace_scene, add_elements,
update_elements, delete_elements, and clear_scene.
`)
}

func serveCommand(args []string, mode runMode) int {
	fs := flag.NewFlagSet(string(mode), flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	listenAddress := fs.String("listen", envOr("EXCALIDRAW_MCP_LISTEN", "127.0.0.1:3210"), "HTTP listen address")
	publicURL := fs.String("public-url", os.Getenv("EXCALIDRAW_MCP_PUBLIC_URL"), "externally reachable browser URL")
	defaultPath := defaultScenePath()
	if mode == modeHTTP {
		defaultPath = envOr("EXCALIDRAW_MCP_SCENE", "-")
	}
	scenePath := fs.String("scene", defaultPath, "persistent .excalidraw file, or - for in-memory state")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *scenePath == "-" {
		*scenePath = ""
	}

	store, err := scene.NewStore(*scenePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: listen:", err)
		return 1
	}
	browserURL := strings.TrimRight(*publicURL, "/")
	if browserURL == "" {
		browserURL = localBrowserURL(listener.Addr())
	}

	var (
		mcpServer *mcp.Server
		httpApp   *httpserver.Server
	)
	if mode == modeWeb {
		httpApp = httpserver.New(store)
	} else {
		mcpServer = newMCPServer(store, browserURL)
		if mode == modeHTTP {
			httpApp = httpserver.NewWithMCP(store, newMCPHTTPHandler(mcpServer))
		} else {
			httpApp = httpserver.New(store)
		}
	}
	httpService := &http.Server{
		Handler:           httpApp.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	httpErr := make(chan error, 1)
	go func() {
		err := httpService.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			httpErr <- err
		}
		close(httpErr)
	}()
	fmt.Fprintln(os.Stderr, "Excalidraw editor:", browserURL)
	if mode == modeHTTP {
		fmt.Fprintln(os.Stderr, "MCP endpoint:", browserURL+"/mcp")
	}
	if *scenePath != "" {
		fmt.Fprintln(os.Stderr, "Scene file:", *scenePath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if mode == modeStdio {
		err = mcpServer.Run(ctx, &mcp.StdioTransport{})
	} else {
		select {
		case <-ctx.Done():
			err = nil
		case err = <-httpErr:
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpService.Shutdown(shutdownCtx)
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func newMCPServer(store *scene.Store, browserURL string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "excalidraw",
		Title:   "Excalidraw MCP",
		Version: version,
	}, nil)
	tools.Register(server, store, browserURL)
	return server
}

func newMCPHTTPHandler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:           true,
			JSONResponse:        true,
			MaxRequestBodyBytes: 64 << 20,
		},
	)
}

func localBrowserURL(address net.Addr) string {
	host, port, err := net.SplitHostPort(address.String())
	if err != nil {
		return "http://" + address.String()
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func defaultScenePath() string {
	if configured := os.Getenv("EXCALIDRAW_MCP_SCENE"); configured != "" {
		return configured
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "scene.excalidraw"
	}
	return filepath.Join(dir, "excalidraw.mcp", "scene.excalidraw")
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
