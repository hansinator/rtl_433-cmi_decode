// cmd/ookdecode/main.go

package main

import (
	"fmt"
	"os"

	"github.com/hansinator/rtl_433-cmi_decode/pkg/framedec"
	"github.com/hansinator/rtl_433-cmi_decode/pkg/ookdec"
	"github.com/hansinator/rtl_433-cmi_decode/pkg/rtl433io"
)

// deduper suppresses repeated frames and only emits
// when a frame is seen at least twice consecutively.
type deduper struct {
	lastHex string
	lastN   int
}

// observe returns true exactly once when the same value
// is seen for the second consecutive time.
func (d *deduper) observe(hex string) (emit bool) {
	if hex == d.lastHex {
		d.lastN++
		// Emit exactly once on the second occurrence.
		return d.lastN == 2
	}

	// New value starts; reset counter.
	d.lastHex = hex
	d.lastN = 1
	return false
}

// handleFrame processes a completed frame from the OOK decoder.
// Only valid 64-bit frames without residue are accepted.
func handleFrame(dd *deduper, frameBits []bool) {
	f := framedec.NewRawFrameFromBits(frameBits, framedec.ResidueLeftAligned)

	// Accept only exactly 64 bits, no residue.
	if f.BitCount != 64 || f.ResidueBits != 0 {
		fmt.Fprintln(os.Stderr, "bad frame:", f.DebugString())
		return
	}

	hex := f.HexString() // "AA BB CC ..."

	// Emit only if seen twice consecutively.
	if dd.observe(hex) {
		// stdout must contain ONLY the hex bytes (pipe-friendly)
		fmt.Fprintln(os.Stdout, hex)
	}
}

// handleDecoderError reports decoder / input errors to stderr.
func handleDecoderError(err error) {
	fmt.Fprintln(os.Stderr, "decoder:", err)
}

func main() {
	var dd deduper

	// Instantiate the concrete decoder via the interface.
	dec := ookdec.NewDCMIDecoder(
		ookdec.BE301Timing(),
		ookdec.BE301SyncConfig(),
	)

	// Run rtl_433 I/O loop.
	if err := rtl433io.Run(
		os.Stdin,
		dec,
		func(bits []bool) { handleFrame(&dd, bits) },
		handleDecoderError,
	); err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		os.Exit(1)
	}

	// If the stream ends and the last frame was seen only once,
	// report it as suspicious.
	if dd.lastHex != "" && dd.lastN == 1 {
		fmt.Fprintln(
			os.Stderr,
			"suspicious: single frame (not repeated):",
			dd.lastHex,
		)
	}
}
