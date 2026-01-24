package ookdec

import "fmt"

/*
 * Decode differential CMI code from BE301 remotes
 */

// Timing thresholds (microseconds), derived from rtl_433 bins.
type Timing struct {
	T2TSplitUS   int // <= split => T, > split => 2T (when data)
	DataMaxUS    int // > DataMaxUS => not data timing (frame-level gaps)
	SyncGapMinUS int // >= => sync gap (between preamble and SOF)
}

func BE301Timing() Timing {
	return Timing{
		T2TSplitUS:   550,
		DataMaxUS:    1800,
		SyncGapMinUS: 4200,
	}
}

type DurClass uint8

const (
	DurUnknown DurClass = iota
	DurT
	Dur2T
	DurInterframe // long gap/idle (but shorter than sync)
	DurSyncGap    // very long gap separating preamble and SOF
)

func (t Timing) Classify(durUS int, gap bool) DurClass {
	// Long gaps are frame markers; in your captures they occur as gaps (low).
	// If you later see long pulses too, remove "gap" from these checks.
	if gap && durUS >= t.SyncGapMinUS {
		return DurSyncGap
	}
	if gap && durUS > t.DataMaxUS {
		return DurInterframe
	}
	// Data timing (either pulse or gap): classify by split.
	if durUS <= 0 {
		return DurUnknown
	}
	if durUS <= t.T2TSplitUS {
		return DurT
	}
	if durUS <= t.DataMaxUS {
		return Dur2T
	}
	return DurUnknown
}

type state uint8

const (
	StIdle      state = iota // waiting for preamble
	StPreamble               // collecting sync word (alternating T durations)
	StExpectSOF              // saw sync gap after valid preamble; next duration must be SOF
	StInFrame                // decoding payload
	StError                  // protocol violation; wait for long idle to reset
)

type DecodeErrorKind uint8

const (
	ErrNone DecodeErrorKind = iota
	ErrBadPreamble
	ErrBadSyncGap
	ErrBadSOF
	ErrTimingInFrame
)

type DecodeError struct {
	Kind  DecodeErrorKind
	State state
	Msg   string
	DurUS int
	Gap   bool
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("decode error kind=%v state=%v dur=%dus gap=%v: %s", e.Kind, e.State, e.DurUS, e.Gap, e.Msg)
}

// Sync/preamble constraints for your protocol.
type SyncConfig struct {
	MinTCount int // minimum number of consecutive T durations in sync word (pulses+gaps)
}

func BE301SyncConfig() SyncConfig {
	return SyncConfig{
		MinTCount: 17, // 9 pulses + 8 gaps (or longer)
	}
}

type DCMIDecoder struct {
	Timing Timing
	Sync   SyncConfig
	State  state

	// Payload decode (time consumption model)
	halfPending bool // one T collected; second T completes a 0

	// Frame bits (payload only)
	inFrameBits []bool

	// Preamble collection
	preCount      int  // number of consecutive DurT elements
	preLastWasGap bool // for alternation check

	// Input stream invariant: pulse/gap must strictly alternate.
	// We only track the immediately previous element.
	haveLast bool
	lastGap  bool
}

// Compile-time check (optional but recommended)
var _ Decoder = (*DCMIDecoder)(nil)

func NewDCMIDecoder(t Timing, s SyncConfig) *DCMIDecoder {
	d := &DCMIDecoder{Timing: t, Sync: s}
	d.resetAll()
	return d
}

func (d *DCMIDecoder) resetAll() {
	d.State = StIdle
	d.halfPending = false
	d.inFrameBits = d.inFrameBits[:0]
	d.haveLast = false
	d.lastGap = false
	d.resetPreamble()
}

func (d *DCMIDecoder) resetPreamble() {
	d.preCount = 0
	d.preLastWasGap = false
}

func (d *DCMIDecoder) enterError(kind DecodeErrorKind, msg string, durUS int, gap bool) *DecodeError {
	prev := d.State
	d.State = StError
	// Drop any partial state; next recovery is on a long idle.
	d.halfPending = false
	d.inFrameBits = d.inFrameBits[:0]
	d.haveLast = false
	d.lastGap = false
	d.resetPreamble()
	return &DecodeError{
		Kind:  kind,
		State: prev,
		Msg:   msg,
		DurUS: durUS,
		Gap:   gap,
	}
}

// Handle consumes one duration element and returns:
// - bits emitted from this element (0..n bits; typically 0 or 1)
// - frameDone indicates we ended a frame (on any long gap > DataMaxUS, including sync gap)
// - frameBits contains the full frame bits when frameDone==true
// - err is non-nil on protocol violations (decoder enters StError)
func (d *DCMIDecoder) Handle(durUS int, gap bool) (bitsOut []bool, frameDone bool, frameBits []bool, err error) {
	// Guard: rtl_433 pulse trains are an interval stream that must alternate pulse/gap.
	// If this triggers, it indicates an upstream/interface bug (wrong gap flag assignment).
	if d.haveLast && gap == d.lastGap {
		return nil, false, nil, d.enterError(
			ErrTimingInFrame,
			"input violates pulse/gap alternation invariant (upstream/interface bug)",
			durUS,
			gap,
		)
	}
	d.haveLast = true
	d.lastGap = gap

	dc := d.Timing.Classify(durUS, gap)

	// Recovery: in error state, ignore everything until we see a long idle (interframe or sync gap).
	if d.State == StError {
		if dc == DurInterframe || dc == DurSyncGap {
			d.resetAll()
		}
		return nil, false, nil, nil
	}

	// 1) Frame termination rule: any long gap (>DataMaxUS) ends an in-progress frame.
	//    This fixes "last gap misclassified as sync and drops last packet".
	if d.State == StInFrame && (dc == DurInterframe || dc == DurSyncGap) {
		// Frame ends; allow halfPending to be true (EOF/padding case).
		if len(d.inFrameBits) > 0 {
			frameDone = true
			frameBits = append([]bool(nil), d.inFrameBits...) // snapshot
		}

		// After a frame, start looking for next preamble (we may already be in a burst).
		d.State = StIdle
		d.halfPending = false
		d.inFrameBits = d.inFrameBits[:0]
		d.resetPreamble()

		// Important: we intentionally do NOT treat this same long gap as "sync gap" for a next frame.
		// It’s just end-of-frame here; next calls will feed preamble elements.
		return nil, frameDone, frameBits, nil
	}

	switch d.State {
	case StIdle:
		// Wait for preamble (DurT run with alternation) to start with a T pulse. Ignore non-T and unknown.
		if (dc == DurT) && !gap {
			d.State = StPreamble
			// initialize preamble run
			d.preCount = 1
			d.preLastWasGap = false
		}
		return nil, false, nil, nil

	case StPreamble:
		switch dc {
		case DurT:
			// Alternation check: consecutive gaps or consecutive pulses indicates foreign/noise.
			// With OOK this basically can't happen and indicates an upstream demodulation error
			if gap == d.preLastWasGap {
				return nil, false, nil, d.enterError(ErrBadPreamble, "preamble does not alternate T pulse/gap", durUS, gap)
			}

			d.preLastWasGap = gap
			d.preCount++
			return nil, false, nil, nil

		case DurSyncGap:
			// Sync gap is only meaningful after a valid preamble run.
			if d.preCount < d.Sync.MinTCount {
				return nil, false, nil, d.enterError(ErrBadSyncGap, "sync gap seen without sufficient preamble T-run", durUS, gap)
			}

			// Verify the sync gap comes after a pulse
			// With OOK this theoretically can't happen and indicates an upstream demodulation error
			if d.preLastWasGap {
				return nil, false, nil, d.enterError(ErrBadPreamble, "sync word should end with HIGH T (pulse), but ended with gap", durUS, gap)
			}

			// Preamble ok; next element must be SOF.
			d.State = StExpectSOF

			// Clear payload-related state explicitly.
			d.halfPending = false
			d.inFrameBits = d.inFrameBits[:0]

			// Reset preamble accumulator for next time (not strictly required).
			d.resetPreamble()
			return nil, false, nil, nil

		case DurInterframe:
			// Idle gap while collecting preamble: treat as reset and wait again.
			d.State = StIdle
			d.resetPreamble()
			return nil, false, nil, nil

		default:
			// Any other timing while collecting preamble is invalid for this protocol.
			return nil, false, nil, d.enterError(ErrBadPreamble, "unexpected timing while collecting preamble", durUS, gap)
		}

	case StExpectSOF:
		// Validate SOF: must be a single T duration pulse
		if gap {
			return nil, false, nil, d.enterError(ErrBadSOF, "SOF must be a pulse (rtl_433 cannot yield a leading gap here)", durUS, gap)
		}

		// If it's not T, it's likely a different protocol/noise.
		if dc != DurT {
			return nil, false, nil, d.enterError(ErrBadSOF, "SOF must be 1T immediately after sync gap", durUS, gap)
		}

		// Start payload decoding AFTER consuming SOF delimiter (SOF is not data).
		d.State = StInFrame
		d.halfPending = false
		d.inFrameBits = d.inFrameBits[:0]
		return nil, false, nil, nil

	case StInFrame:
		// Decode by time consumption, ignoring polarity.
		switch dc {
		case DurT:
			if d.halfPending {
				d.halfPending = false
				d.inFrameBits = append(d.inFrameBits, false)
				bitsOut = append(bitsOut, false)
			} else {
				d.halfPending = true
			}
			return bitsOut, false, nil, nil

		case Dur2T:
			// 2T completes a full bit immediately. If we had a pending half, treat as protocol violation.
			if d.halfPending {
				return nil, false, nil, d.enterError(ErrTimingInFrame, "alignment error: 2T encountered with pending half T", durUS, gap)
			}
			d.inFrameBits = append(d.inFrameBits, true)
			bitsOut = append(bitsOut, true)
			return bitsOut, false, nil, nil

		case DurUnknown:
			return nil, false, nil, d.enterError(ErrTimingInFrame, "unknown duration class in frame", durUS, gap)

		default:
			// Note: DurInterframe/DurSyncGap in frame are handled at top as frame termination.
			return nil, false, nil, d.enterError(ErrTimingInFrame, "unexpected non-data duration class in frame", durUS, gap)
		}

	default:
		return nil, false, nil, d.enterError(ErrTimingInFrame, "invalid state", durUS, gap)
	}
}
