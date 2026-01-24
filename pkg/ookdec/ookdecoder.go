// package ookdec

package ookdec

// Decoder is the common interface for OOK decoders that consume pulse/gap durations.
type Decoder interface {
	// Handle consumes one duration element (microseconds) and whether it is a gap.
	// bitsOut are bits emitted by this element (usually 0 or 1 bit).
	// frameDone indicates a frame ended; frameBits contains the full frame when done.
	Handle(durUS int, gap bool) (bitsOut []bool, frameDone bool, frameBits []bool, err error)
}
