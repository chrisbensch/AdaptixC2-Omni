package naxbuilder

import "testing"

func TestComponentPath(t *testing.T) {
	cases := []struct {
		transport string
		debug     bool
		name      string
		want      string
	}{
		{"http", false, "loader", "src_loader/bin/nax_loader.x64.bin"},
		{"smb", false, "loader", "src_loader/bin/nax_loader.x64.bin"},
		{"http", false, "beacon", "src_beacon/build/http/beacon.x64.bin"},
		{"smb", false, "beacon", "src_beacon/build/smb/beacon.x64.bin"},
		{"http", true, "beacon", "src_beacon/build/http/beacon.x64.debug.bin"},
		{"smb", true, "beacon", "src_beacon/build/smb/beacon.x64.debug.bin"},
		{"http", false, "pdata", "src_beacon/build/http/beacon.pdata.bin"},
		{"smb", false, "pdata", "src_beacon/build/smb/beacon.pdata.bin"},
		{"http", true, "pdata", "src_beacon/build/http/beacon.debug.pdata.bin"},
		{"http", false, "xdata", "src_beacon/build/http/beacon.xdata.bin"},
		{"http", true, "xdata", "src_beacon/build/http/beacon.debug.xdata.bin"},
		{"http", false, "textRva", "src_beacon/build/http/beacon.text_rva"},
		{"http", true, "textRva", "src_beacon/build/http/beacon.debug.text_rva"},
		{"http", false, "sleepmask", "src_sleepmask/dist/sleepmask.x64.o"},
		{"http", false, "bogus", ""}, // unknown component -> empty
	}

	for _, c := range cases {
		if got := ComponentPath(c.transport, c.debug, c.name); got != c.want {
			t.Errorf("ComponentPath(%q, %v, %q) = %q, want %q", c.transport, c.debug, c.name, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	valids := []NaxBuildRequest{
		{Transport: "http", OutputFormat: "bin", EncKeyHex: "00112233445566778899aabbccddeeff"},
		{Transport: "smb", OutputFormat: "svc"},                  // no encKeyHex -> optional, allowed
		{Transport: "http", OutputFormat: "exe"},                 // defaults elsewhere
	}
	for _, r := range valids {
		if err := r.Validate(); err != nil {
			t.Errorf("valid request %+v rejected: %v", r, err)
		}
	}

	if err := (&NaxBuildRequest{Transport: "ftp"}).Validate(); err == nil {
		t.Error("bad transport should be rejected")
	}
	if err := (&NaxBuildRequest{Transport: "http", OutputFormat: "raw"}).Validate(); err == nil {
		t.Error("bad outputFormat should be rejected")
	}
	if err := (&NaxBuildRequest{Transport: "http", OutputFormat: "bin", EncKeyHex: "deadbeef"}).Validate(); err == nil {
		t.Error("short encKeyHex should be rejected")
	}
	if err := (&NaxBuildRequest{Transport: "http", OutputFormat: "bin", EncKeyHex: "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"}).Validate(); err == nil {
		t.Error("non-hex encKeyHex should be rejected")
	}
}
