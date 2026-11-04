package main

import (
	"encoding/json"
	"fmt"
	"io"
)

func cmdNavIconKeys(args []string, out io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(out, "usage: ocserverd nav-icon-keys")
		return 2
	}
	if err := json.NewEncoder(out).Encode(navIconAllowedKeys()); err != nil {
		fmt.Fprintf(out, "[nav-icon-keys] encode allowlist: %v\n", err)
		return 1
	}
	return 0
}
