package naxbuilder

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// buildMu serializes builds in this prototype (a build spawns process-heavy make
// + toolchain steps); it is removed once the worker serves concurrent requests.
var buildMu sync.Mutex

// StartListener listens on the given Unix socket path and serves one
// request/response exchange per connection using handleRequest. It blocks until
// the listener fails or is closed.
func StartListener(sockPath string) {
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		panic(fmt.Errorf("builder: listen on %q: %w", sockPath, err))
	}
	// net.Listen creates the socket file 0o755 (no group write), which blocks
	// connect() for a non-owner user. The teamserver runs as `adaptix` (UID 10001)
	// joined to the builder's `naxb` group (GID 10002) and dials this socket over
	// the shared /run/nax volume, so widen it to 0o660 (owner + naxb group rw).
	// Best-effort: if the path isn't a regular file (e.g. a symlink race) skip.
	if fi, err := os.Stat(sockPath); err == nil && fi.Mode()&os.ModeSymlink == 0 {
		_ = os.Chmod(sockPath, 0o660)
	}
	defer l.Close()
	for {
		conn, err := l.Accept()
		if err != nil {
			return // listener closed / failed
		}
		go ServeConn(conn, handleRequest)
	}
}

// handleRequest validates and builds a payload. For the bin output it returns the
// raw components (the server repacks them with packNaxBin); for a non-bin format
// (exe/dll/svc) it produces the packed PIC via the make combined-output target and
// wraps it in the requested PE format.
func handleRequest(req *NaxBuildRequest) (*NaxBuildResponse, error) {
	buildMu.Lock()
	defer buildMu.Unlock()

	if err := req.Validate(); err != nil {
		return nil, err // ServeConn serializes this to a NaxBuildError frame.
	}

	if req.OutputFormat == "" || req.OutputFormat == "bin" {
		return BuildComponents(naxRoot, req)
	}

	packed, err := buildPIC(naxRoot, req)
	if err != nil {
		return nil, err
	}
	pb, peName, err := compileWrapper(packed, req.OutputFormat, req.SvcName, req.DllExport, req.Debug, nil)
	if err != nil {
		return nil, err
	}
	return &NaxBuildResponse{
		Filename:   peName,
		Components: map[string][]byte{"payload": pb},
		OK:         true,
	}, nil
}

// buildPIC runs the make combined-output target (all/debug/link), which runs the
// python pack script to produce the packed binary, then reads it back.
func buildPIC(root string, req *NaxBuildRequest) ([]byte, error) {
	target := combinedTarget(req)
	if err := runMake(root, target, makeArgs(req)); err != nil {
		return nil, fmt.Errorf("make %s: %w", target, err)
	}
	outName := "build/nax.x64.bin"
	if combinedTargetIsDebug(target) {
		outName = "build/nax.x64.debug.bin"
	}
	b, err := os.ReadFile(filepath.Join(root, outName))
	if err != nil {
		return nil, fmt.Errorf("read combined output %s: %w", outName, err)
	}
	return b, nil
}

// combinedTarget maps req onto the make combined-output targets (all/debug/link).
func combinedTarget(req *NaxBuildRequest) string {
	switch {
	case req.Debug && req.FullRebuild:
		return "debug-link"
	case req.Debug:
		return "debug"
	case req.FullRebuild:
		return "link"
	default:
		return "all"
	}
}

func combinedTargetIsDebug(target string) bool {
	return target == "debug" || target == "debug-link"
}

// ResolveSocketPath returns the builder socket path for the given module dir: it
// reads moduleDir/nax_builder_socket (its trimmed, absolutized contents) if that
// file exists; otherwise it falls back to /run/nax/builder.sock.
func ResolveSocketPath(moduleDir string) string {
	conf := filepath.Join(moduleDir, "nax_builder_socket")
	if data, err := os.ReadFile(conf); err == nil {
		if dir, err := filepath.Abs(strings.TrimSpace(string(data))); err == nil {
			return dir
		}
	}
	return "/run/nax/builder.sock"
}

// Client dials a builder over a Unix socket per Build call.
type Client struct {
	sock string
}

// New returns a client for the given socket path. It does not dial eagerly, so it
// succeeds whether or not a builder is currently listening; Build performs the dial.
func New(sock string) *Client { return &Client{sock: sock} }

// Build dials the builder, sends one request frame, and reads the response. Dial /
// transport errors are returned directly; a builder error response (OK:false, or an
// error frame) becomes a Go error carrying the builder's message.
func (c *Client) Build(req *NaxBuildRequest) (*NaxBuildResponse, error) {
	conn, err := net.Dial("unix", c.sock)
	if err != nil {
		return nil, fmt.Errorf("connect to builder socket %q: %w", c.sock, err)
	}
	defer conn.Close()

	if err := WriteFrame(conn, req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	body, err := readFrameBody(conn, MaxFrameBytes)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var probe struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &probe)
	if probe.OK == nil || !*probe.OK {
		return nil, errors.New(probe.Error) // builder returned an error response
	}

	var resp NaxBuildResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}
