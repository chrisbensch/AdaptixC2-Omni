package kharonbuilder

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	valids := []KharonBuildRequest{
		{Target: "x64", OutputFormat: "bin"},
		{Target: "x86", OutputFormat: "svc"}, // defaults elsewhere
		{Target: "x64", OutputFormat: "exe"}, // defaults elsewhere
		{Target: "x86", OutputFormat: "dll"}, // defaults elsewhere
	}
	for _, r := range valids {
		if err := r.Validate(); err != nil {
			t.Errorf("valid request %+v rejected: %v", r, err)
		}
	}

	if err := (&KharonBuildRequest{Target: "arm64"}).Validate(); err == nil {
		t.Error("bad target should be rejected")
	}
	if err := (&KharonBuildRequest{Target: "x64", OutputFormat: "raw"}).Validate(); err == nil {
		t.Error("bad outputFormat should be rejected")
	}
}

func TestMakeTarget(t *testing.T) {
	cases := []struct {
		req  KharonBuildRequest
		want string
	}{
		{KharonBuildRequest{Target: "x64"}, "x64"},
		{KharonBuildRequest{Target: "x86"}, "x86"},
		{KharonBuildRequest{Target: "x64", Debug: true}, "x64-debug"},
		{KharonBuildRequest{Target: "x86", Debug: true}, "x86-debug"},
	}
	for _, c := range cases {
		if got := c.req.makeTarget(); got != c.want {
			t.Errorf("makeTarget(%+v) = %q, want %q", c.req, got, c.want)
		}
	}
}

func TestMakeArgs(t *testing.T) {
	r := KharonBuildRequest{
		WebSecureEnabled:  true,
		WebProxyEnabled:   false,
		WebProxyURL:       "proxy:8080",
		KhSleepTime:       "5000",
		KhJitter:          25,
		KhAgentUUID:       "uuid-123",
		KhKilldateEnabled: true,
		KhKilldateDay:     15,
		KhKilldateMonth:   8,
		KhKilldateYear:    2026,
		KhForkPipeName:    `\\..\pipe\kh`,
		KhSpawnto:         "notepad.exe",
		KhBofHookEnabled:  true,
		HTTPMalleableHex:  "{ 0x01, 0x02 }",
		HTTPCallbackCount: 3,
		KhGuardrailHost:   "guard.host",
		KhSyscall:         2,
		KhAmsiEtwBypass:   0x2,
		KhHeapMask:        true,
		KhSleepMask:       1,
	}
	got := r.makeArgs()

	// spot-check a few representative entries (order-independent membership).
	// The forkpipe value is checked by prefix + value-substring to avoid a
	// backslash-escaping minefield in the test source.
	wantSubstrings := []string{
		"WEB_SECURE_ENABLED=1",
		"WEB_PROXY_ENABLED=0",
		"WEB_PROXY_URL=proxy:8080",
		"KH_SLEEP_TIME=5000",
		"KH_JITTER=25",
		"KH_AGENT_UUID=uuid-123",
		"KH_KILLDATE_ENABLED=1",
		"KH_KILLDATE_DAY=15",
		"KH_KILLDATE_YEAR=2026",
		"KH_SPAWNTO_X64=\"notepad.exe\"",
		"KH_BOF_HOOK_ENABLED=1",
		"HTTP_CALLBACK_COUNT=3",
		"KH_GUARDRAILS_HOST=guard.host",
		"KH_SYSCALL=2",
		"KH_AMSI_ETW_BYPASS=0x2",
		"KH_HEAP_MASK=1",
		"KH_SLEEP_MASK=1",
	}
	for _, w := range wantSubstrings {
		if !contains(got, w) {
			t.Errorf("makeArgs missing %q; got %v", w, got)
		}
	}
	// The forkpipe value is wrapped in double quotes; check the prefix + value.
	if !strings.Contains(gotStr(got), "KH_FORK_PIPENAME=") || !strings.Contains(gotStr(got), `kh`) {
		t.Errorf("missing KH_FORK_PIPENAME entry; got %v", got)
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// gotStr joins make-arg entries into one string for substring checks (used for
// values that carry spaces/quotes so exact-match contains() can't find them).
func gotStr(got []string) string {
	return strings.Join(got, " ")
}

func TestMakeArgsOmitsEmptyGuardrails(t *testing.T) {
	r := KharonBuildRequest{Target: "x64"} // no guardrails set
	for _, a := range r.makeArgs() {
		if len(a) >= len("KH_GUARDRAILS_") && a[:len("KH_GUARDRAILS_")] == "KH_GUARDRAILS_" {
			t.Errorf("empty guardrail emitted: %q", a)
		}
	}
}

func TestOutFileName(t *testing.T) {
	cases := []struct {
		format string
		target string
		want   string
	}{
		{"exe", "x64", "Kharon.x64.exe"},
		{"dll", "x64", "Kharon.x64.dll"},
		{"svc", "x64", "Kharon.x64.svc.exe"},
		{"bin", "x64", "Kharon.x64.bin"},
		{"bin", "x86", "Kharon.x64.bin"}, // bin filename is always x64 (see pl_agent.go)
		{"", "x86", "Kharon.x86.bin"},    // default -> Kharon.<target>.bin
	}
	for _, c := range cases {
		got := (KharonBuildRequest{Target: c.target, OutputFormat: c.format}).outFileName()
		if got != c.want {
			t.Errorf("outFileName(format=%q,target=%q) = %q, want %q", c.format, c.target, got, c.want)
		}
	}
}

func TestLoaderSource(t *testing.T) {
	cases := []struct {
		format string
		src    string
		out    string
		ok     bool
	}{
		{"exe", "Exe.cc", "Kharon.x64.exe", true},
		{"dll", "Dll.cc", "Kharon.x64.dll", true},
		{"svc", "Svc.cc", "Kharon.x64.svc.exe", true},
		{"bin", "", "", false},
		{"bogus", "", "", false},
	}
	for _, c := range cases {
		src, out, ok := (KharonBuildRequest{OutputFormat: c.format}).loaderSource()
		if src != c.src || out != c.out || ok != c.ok {
			t.Errorf("loaderSource(format=%q) = (%q,%q,%v), want (%q,%q,%v)",
				c.format, src, out, ok, c.src, c.out, c.ok)
		}
	}
}
