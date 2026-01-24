package rtl433io

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/hansinator/rtl_433-cmi_decode/pkg/ookdec"
)

// Run reads rtl_433 pulse/gap integers from r, feeds dec, and calls onFrame when a frame completes.
//
// Strategy (as requested):
//   - Trim line
//   - Ignore empty and ';' comment lines
//   - Keep ONLY lines that have exactly two fields separated by exactly one ASCII space
//     (i.e. no tabs, no multiple spaces)
//   - Parse both fields as ints; if parsing fails, report error (like before)
//   - Feed pulse then gap to decoder
//
// Notes:
//   - This function is intentionally strict, because rtl_433 produces many banner/log lines,
//     sometimes with ANSI colors. Those lines will almost never match "INT␠INT".
//   - If rtl_433 ever changes to emit "INT<spaces>INT" (multiple spaces or tabs), this filter
//     would start dropping data. In that case, loosen this parser and add a different log filter.
func Run(r io.Reader, dec ookdec.Decoder, onFrame func([]bool), onDecoderErr func(error)) error {
	in := bufio.NewScanner(r)
	// Default Scanner token limit is 64K; rtl_433 can emit large output in some modes.
	// We don't expect huge lines here, but keep the buffer generous to be safe.
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		// Require exactly one ASCII space and no tabs / extra spaces.
		// This excludes rtl_433 banner/log lines and the ANSI-colored "[SDR] ..." lines.
		if strings.Count(line, " ") != 1 || strings.ContainsAny(line, "\t") {
			continue
		}

		// Split into exactly two fields. Note: we intentionally do NOT use strings.Fields,
		// because Fields would accept multiple spaces and tabs, and then we'd ingest
		// lines we intended to ignore.
		parts := strings.Split(line, " ")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			continue
		}

		// Parse pulse and gap durations (microseconds).
		p, err := strconv.Atoi(parts[0])
		if err != nil {
			if onDecoderErr != nil {
				onDecoderErr(fmt.Errorf("rtl433io: atoi %q: %w", parts[0], err))
			}
			continue
		}
		g, err := strconv.Atoi(parts[1])
		if err != nil {
			if onDecoderErr != nil {
				onDecoderErr(fmt.Errorf("rtl433io: atoi %q: %w", parts[1], err))
			}
			continue
		}

		// Sanity: durations should be positive. If rtl_433 ever emits 0 or negatives,
		// treat it as input corruption and skip.
		if p <= 0 || g <= 0 {
			if onDecoderErr != nil {
				onDecoderErr(fmt.Errorf("rtl433io: non-positive duration(s): pulse=%d gap=%d", p, g))
			}
			continue
		}

		// Feed pulse.
		// Frames normally end on long gaps, so a frame ending right after a pulse is unexpected.
		_, frameDone, frameBits, frameErr := dec.Handle(p, false)
		if frameErr != nil && onDecoderErr != nil {
			onDecoderErr(frameErr)
		}
		if frameDone {
			if onDecoderErr != nil {
				onDecoderErr(fmt.Errorf("rtl433io: frame ended on pulse (unexpected)"))
			}
			onFrame(frameBits)
		}

		// Feed gap.
		_, frameDone, frameBits, frameErr = dec.Handle(g, true)
		if frameErr != nil && onDecoderErr != nil {
			onDecoderErr(frameErr)
		}
		if frameDone {
			onFrame(frameBits)
		}
	}

	// Propagate scanner errors (I/O errors etc.).
	return in.Err()
}
