package main

import (
	"os"

	"github.com/entropy-z/AdaptixC2-Omni/sidecar/kharon-builder/kharonbuilder"
)

func main() {
	// Where the Kharon source tree lives in this image (default
	// /app/kharon — the builder image lays the pinned Kharon source there). The
	// worker uses it for both the beacon make and the loader compile.
	src := os.Getenv("KHARON_SRC")
	if src == "" {
		src = "/app/kharon"
	}
	kharonbuilder.SetKharonRoot(src)

	// The shared unix socket (a named volume /run/kharon, mounted in compose). The
	// teamserver dials this; the worker writes the socket to it.
	sock := os.Getenv("KHARON_BUILDER_SOCK")
	if sock == "" {
		sock = "/run/kharon/builder.sock"
	}
	kharonbuilder.StartListener(sock)
}
