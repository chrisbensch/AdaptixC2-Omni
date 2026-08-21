package naxbuilder

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	req := NaxBuildRequest{
		Transport:  "http",
		Debug:      true,
		OutputFormat: "bin",
		EncKeyHex:  "00112233445566778899aabbccddeeff",
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, &req); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 || buf.Len() > MaxFrameBytes {
		t.Fatalf("unexpected frame size %d", buf.Len())
	}

	var got NaxBuildRequest
	if err := ReadFrame(&buf, &got, MaxFrameBytes); err != nil {
		t.Fatal(err)
	}
	if got.Transport != req.Transport || got.OutputFormat != req.OutputFormat || got.EncKeyHex != req.EncKeyHex {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestFrameTooLargeRejected(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, &NaxBuildRequest{Transport: "http"}); err != nil {
		t.Fatal(err)
	}

	// A 2-byte cap must reject the ~15-byte body.
	if err := ReadFrame(&buf, &NaxBuildRequest{}, 2); err == nil {
		t.Fatal("expected error for oversized frame")
	} else if err != errFrameTooLarge {
		t.Fatalf("expected errFrameTooLarge, got %v", err)
	}
}
