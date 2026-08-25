package naxbuilder

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// naxRoot is the NaX source root the builder worker uses to locate pe_templates
// and (via BuildComponents) the Makefile. It is set once at worker startup (the
// baked-in source tree inside the builder image). Tests point it at a fixture or
// leave it empty (which yields a clear "pe_templates not found" error).
var naxRoot = ""

// SetNaxRoot points the worker at the NaX source root (where the Makefile and
// src_server/agent_nonameax/pe_templates live). Called once at startup; empty
// falls back to "/nax" (the builder image's layout). The tests override it with
// a fixture dir.
func SetNaxRoot(p string) {
	if p == "" {
		p = "/nax"
	}
	naxRoot = p
}

// generateShellcodeH renders a PIC shellcode blob as a C++ header with a .text
// section holding the bytes as hex literals. It mirrors pl_build.go's version;
// the only deviation is lower-case hex digits, which is cosmetically irrelevant
// to the compiled PE.
func generateShellcodeH(blob []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("#pragma once\n\n")
	buf.WriteString("namespace Shellcode {\n")
	buf.WriteString("    __attribute__((used, section(\".text\")))\n")
	buf.WriteString("    unsigned char Data[] = {\n")

	for i, b := range blob {
		if i%16 == 0 {
			buf.WriteString("        ")
		}
		fmt.Fprintf(&buf, "0x%02x,", b)
		if i%16 == 15 || i == len(blob)-1 {
			buf.WriteString("\n")
		} else {
			buf.WriteByte(' ')
		}
	}

	buf.WriteString("    };\n")
	buf.WriteString("}\n")
	return buf.Bytes()
}

// compileWrapper writes Shellcode.h into a temp dir, resolves pe_templates
// relative to naxRoot, and compiles the PIC payload into an exe/dll/svc PE using
// the mingw toolchain (invoked with an explicit arg array, no shell). If the
// mingw compiler is not on PATH it returns a clear "mingw toolchain absent" error
// before any compilation is attempted.
func compileWrapper(pic []byte, format, svcName, dllExport string, debug bool, logFn func(int, string, ...any)) ([]byte, string, error) {
	compiler := "x86_64-w64-mingw32-g++"
	if _, err := exec.LookPath(compiler); err != nil {
		return nil, "", fmt.Errorf("mingw toolchain %q is absent (install mingw-w64): %w", compiler, err)
	}
	if logFn != nil {
		logFn(1, "PE wrapper: %s %s", compiler, format)
	}

	tmpDir, err := os.MkdirTemp("", "nax_pe_wrap_")
	if err != nil {
		return nil, "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "Shellcode.h"), generateShellcodeH(pic), 0o644); err != nil {
		return nil, "", fmt.Errorf("write Shellcode.h: %w", err)
	}

	tplDir := filepath.Join(naxRoot, "src_server", "agent_nonameax", "pe_templates")
	if _, err := os.Stat(tplDir); err != nil {
		return nil, "", fmt.Errorf("pe_templates not found at %s (naxRoot=%q)", tplDir, naxRoot)
	}

	var sourceFile, outName string
	var extraFlags []string
	switch format {
	case "exe":
		sourceFile = filepath.Join(tplDir, "Exe.cc")
		outName = "nax.x64.exe"
	case "dll":
		sourceFile = filepath.Join(tplDir, "Dll.cc")
		outName = "nax.x64.dll"
		extraFlags = append(extraFlags, "-shared")
	case "svc":
		sourceFile = filepath.Join(tplDir, "Svc.cc")
		outName = "nax.x64.svc.exe"
		extraFlags = append(extraFlags, "-ladvapi32")
	default:
		return nil, "", fmt.Errorf("unknown PE format: %s", format)
	}

	if debug {
		outName = strings.Replace(outName, "nax.x64", "nax.x64.debug", 1)
	}
	outPath := filepath.Join(tmpDir, outName)

	subsystem := "-mwindows"
	if debug {
		subsystem = "-mconsole"
	}

	args := []string{
		"-I", tmpDir,
		"-I", tplDir,
		"-o", outPath,
		sourceFile,
		"-Os", subsystem,
		"-nostdlib", "-s",
		"-Wl,-eWinMainCRTStartup",
		"-lkernel32",
	}
	if format == "svc" {
		args = append(args, fmt.Sprintf("-DNAX_SVC_NAME=L\"%s\"", svcName))
	}
	args = append(args, extraFlags...)
	if format == "dll" && dllExport != "" && dllExport != "Runner" {
		defPath := filepath.Join(tmpDir, "exports.def")
		if err := os.WriteFile(defPath, []byte(fmt.Sprintf("EXPORTS\n  %s=Runner\n", dllExport)), 0o644); err != nil {
			return nil, "", fmt.Errorf("write exports.def: %w", err)
		}
		args = append(args, defPath)
	}

	cmdOut, cmdErr := exec.Command(compiler, args...).CombinedOutput()
	if cmdErr != nil {
		return nil, "", fmt.Errorf("%s failed: %v\n%s", compiler, cmdErr, string(cmdOut))
	}

	peBytes, err := os.ReadFile(outPath)
	if err != nil {
		return nil, "", fmt.Errorf("read output PE: %w", err)
	}
	if logFn != nil {
		logFn(3, "PE wrapper: %s built (%d bytes)", outName, len(peBytes))
	}
	return peBytes, outName, nil
}
