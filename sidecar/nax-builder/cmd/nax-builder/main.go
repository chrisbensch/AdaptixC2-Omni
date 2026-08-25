package main

import (
	"os"

	"github.com/entropy-z/AdaptixC2-Omni/sidecar/nax-builder/naxbuilder"
)

func main() {
	// Where the NaX source tree lives in this image (default /nax — the
	// builder image lays the pinned NaX source there). The worker uses it for
	// both the `make` targets and the pe_templates/ PE wrapper location.
	src := os.Getenv("NAX_SRC")
	if src == "" {
		src = "/nax"
	}
	naxbuilder.SetNaxRoot(src)

	// The shared unix socket (a named volume /run/nax, mounted in compose). The
	// teamserver dials this; the worker writes the socket to it.
	sock := os.Getenv("NAX_BUILDER_SOCK")
	if sock == "" {
		sock = "/run/nax/builder.sock"
	}
	naxbuilder.StartListener(sock)
}
