package framedec

import (
	"fmt"
	"strings"
)

// Residue alignment choice:
//   - Left-aligned (MSB): first residue bit goes to 0x80. This is best for human hex dumps,
//     and matches how full bytes are typically shown MSB-first.
//   - Right-aligned (LSB): residue bits occupy the low bits; useful if you want to treat the
//     residue as an integer value.
//
// For debugging/hex dumps, left-aligned is usually better because the bit stream reads
// consistently across byte boundaries.
type ResidueAlign uint8

const (
	ResidueLeftAligned ResidueAlign = iota
	ResidueRightAligned
)

type RawFrame struct {
	BitCount    int    // total bits
	Bytes       []byte // full bytes
	ResidueBits int    // 0..7
	Residue     byte   // partial byte; aligned according to Align
	Align       ResidueAlign
}

// NewRawFrameFromBits packs bits into bytes.
// Packing is MSB-first within each byte: first bit becomes 0x80, next 0x40, ...
func NewRawFrameFromBits(bits []bool, align ResidueAlign) RawFrame {
	bitLen := len(bits)
	fullBytes := bitLen / 8
	residueBits := bitLen % 8

	out := RawFrame{
		BitCount:    bitLen,
		Bytes:       make([]byte, fullBytes),
		ResidueBits: residueBits,
		Residue:     0,
		Align:       align,
	}

	// Full bytes
	for i := 0; i < fullBytes; i++ {
		var b byte
		base := i * 8
		for j := 0; j < 8; j++ {
			if bits[base+j] {
				b |= 1 << (7 - j)
			}
		}
		out.Bytes[i] = b
	}

	// Residue
	if residueBits != 0 {
		base := fullBytes * 8
		switch align {
		case ResidueLeftAligned:
			// residue bits occupy MSBs: bit0 -> 0x80, bit1 -> 0x40, ...
			for j := 0; j < residueBits; j++ {
				if bits[base+j] {
					out.Residue |= 1 << (7 - j)
				}
			}
		case ResidueRightAligned:
			// residue bits occupy LSBs: last residue bit -> 0x01
			shift := residueBits - 1
			for j := 0; j < residueBits; j++ {
				if bits[base+j] {
					out.Residue |= 1 << shift
				}
				shift--
			}
		default:
			// default to left aligned
			for j := 0; j < residueBits; j++ {
				if bits[base+j] {
					out.Residue |= 1 << (7 - j)
				}
			}
		}
	}

	return out
}

// HexString returns a hex dump identical to the old handleFrame output style.
// If residue exists, it prints the partial byte as "XX/<n>" where n is ResidueBits.
func (f RawFrame) HexString() string {
	var sb strings.Builder

	for i, b := range f.Bytes {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%02X", b)
	}

	if f.ResidueBits != 0 {
		if len(f.Bytes) > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%02X/%d", f.Residue, f.ResidueBits)
	}

	return sb.String()
}

// DebugString prints a compact, stable summary.
func (f RawFrame) DebugString() string {
	return fmt.Sprintf(
		"bits=%d bytes=%d residue=%d hex=%s",
		f.BitCount,
		len(f.Bytes),
		f.ResidueBits,
		f.HexString(),
	)
}

// DebugPrint prints DebugString() to stdout.
func (f RawFrame) DebugPrint() {
	fmt.Println(f.DebugString())
}
