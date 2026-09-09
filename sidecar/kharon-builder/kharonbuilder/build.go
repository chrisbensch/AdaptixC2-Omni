package kharonbuilder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// logsCap bounds the accumulated build logs carried back to the server so a
// runaway build can't blow up the response frame.
const logsCap = 64 << 10

// runMake drives the Kharon beacon Makefile. It is a package variable so tests can
// stub it to a no-op; in production it runs
// `make -C <kharonRoot>/src_beacon <target> <vars…>`, which needs the mingw/nasm/
// make toolchain.
var runMake = func(root string, target string, vars []string) (string, error) {
	args := []string{"-C", root, target}
	args = append(args, vars...)
	cmd := exec.Command("make", args...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return out.String() + errb.String(), fmt.Errorf("make %s: %w", target, err)
	}
	return out.String() + errb.String(), nil
}

// buildBeacon runs the beacon make target and reads the raw PIC .bin it produces.
// The Kharon Makefile emits Bin/Kharon.<arch>.bin (objcopy of the temp .exe); the
// -debug suffix only adds -D DEBUG to CFLAGS and does not change the output name.
func buildBeacon(req *KharonBuildRequest) ([]byte, string, error) {
	beaconRoot := filepath.Join(kharonRoot, "src_beacon")
	logs, err := runMake(beaconRoot, req.makeTarget(), req.makeArgs())
	if err != nil {
		return nil, "", trimLogs(fmt.Errorf("beacon make: %w", err), logs)
	}
	outName := fmt.Sprintf("Kharon.%s.bin", req.Target)
	binPath := filepath.Join(beaconRoot, "Bin", outName)
	b, err := os.ReadFile(binPath)
	if err != nil {
		return nil, "", trimLogs(fmt.Errorf("read beacon %s: %w", outName, err), logs)
	}
	return b, logs, nil
}

// build runs the full Kharon payload build: produce the beacon, then (for non-bin
// formats) wrap it in a loader PE. Returns the final payload bytes, the output
// filename, and accumulated build logs.
func build(req *KharonBuildRequest) (*KharonBuildResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	packed, logs, err := buildBeacon(req)
	if err != nil {
		return nil, err
	}

	// bin: the raw PIC beacon is the payload.
	if req.OutputFormat == "" || req.OutputFormat == "bin" {
		return finalize(&KharonBuildResponse{
			Filename: req.outFileName(),
			Payload:  packed,
			OK:       true,
			Logs:     logs,
		}), nil
	}

	// exe/dll/svc: wrap the beacon in a loader PE.
	peBytes, outName, err := compileWrapper(req, packed)
	if err != nil {
		return nil, trimLogs(fmt.Errorf("loader compile: %w", err), logs)
	}
	return finalize(&KharonBuildResponse{
		Filename: outName,
		Payload:  peBytes,
		OK:       true,
		Logs:     logs,
	}), nil
}

// finalize computes Size + SHA256 over the payload.
func finalize(resp *KharonBuildResponse) *KharonBuildResponse {
	sum := sha256.Sum256(resp.Payload)
	resp.SHA256 = hex.EncodeToString(sum[:])
	resp.Size = len(resp.Payload)
	return resp
}

// trimLogs caps the accumulated build logs to logsCap bytes.
func trimLogs(err error, logs string) error {
	if len(logs) > logsCap {
		logs = logs[:logsCap] + "\n…[truncated build logs]"
	}
	return fmt.Errorf("%s\nbuild logs:\n%s", err, logs)
}
