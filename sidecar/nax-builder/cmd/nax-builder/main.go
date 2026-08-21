package main

import (
	"os"

	"github.com/entropy-z/AdaptixC2-Omni/sidecar/nax-builder/naxbuilder"
)

func main() {
	sock := os.Getenv("NAX_BUILDER_SOCK")
	if sock == "" {
		sock = "/run/nax/builder.sock"
	}
	naxbuilder.StartListener(sock)
}
