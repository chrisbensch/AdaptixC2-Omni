package kharonbuilder

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	req := KharonBuildRequest{
		Target:       "x64",
		Debug:        true,
		OutputFormat: "bin",
		KhAgentUUID:  "test-uuid",
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, &req); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 || buf.Len() > MaxFrameBytes {
		t.Fatalf("unexpected frame size %d", buf.Len())
	}

	var got KharonBuildRequest
	if err := ReadFrame(&buf, &got, MaxFrameBytes); err != nil {
		t.Fatal(err)
	}
	if got.Target != req.Target || got.Debug != req.Debug || got.OutputFormat != req.OutputFormat || got.KhAgentUUID != req.KhAgentUUID {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestFrameTooLargeRejected(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, &KharonBuildRequest{Target: "x64"}); err != nil {
		t.Fatal(err)
	}

	// A 2-byte cap must reject the ~15-byte body.
	if err := ReadFrame(&buf, &KharonBuildRequest{}, 2); err == nil {
		t.Fatal("expected error for oversized frame")
	} else if err != errFrameTooLarge {
		t.Fatalf("expected errFrameTooLarge, got %v", err)
	}
}
