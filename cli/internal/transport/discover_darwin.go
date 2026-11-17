package transport

import (
	"context"
	"path/filepath"
)

// macOS fallback deliberately only probes CDC usbmodem ports. Native USB
// descriptor enumeration would require cgo/IOKit; HELLO + INFO still validate
// product, protocol and stable firmware serial before selecting a device.
func Discover(ctx context.Context) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names, err := filepath.Glob("/dev/cu.usbmodem*")
	out := []Candidate{}
	for _, name := range names {
		out = append(out, Candidate{Port: name})
	}
	return out, err
}
