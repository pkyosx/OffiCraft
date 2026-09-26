package main

import (
	"embed"
	"io/fs"
)

// The TCC identity anchor, staged in by bin/build-bindist (the .gitkeep-placeholder
// staging contract, so a plain `go build` on a clean checkout still compiles —
// it just carries no anchor).
//
// ocwarden carries a copy because the cockpit's new-machine one-liner downloads
// ocwarden ALONE and `ocwarden install` refuses without an anchor. The bytes must
// be identical to dist/officraft and the tarball's copy: those bytes ARE the TCC
// identity, and a different copy means a separate authorization prompt.
//
//go:embed all:anchordist
var anchorEmbed embed.FS

var embeddedAnchor = func() []byte {
	b, err := fs.ReadFile(anchorEmbed, "anchordist/officraft")
	if err != nil {
		return nil
	}
	return b
}
