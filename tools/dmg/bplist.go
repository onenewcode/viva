package dmg

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf16"
)

// The Finder keeps its window and icon view settings as binary property lists
// inside .DS_Store. Only a dictionary of scalars is ever needed, so this
// writes exactly that rather than being a property list encoder.

type plistKind uint8

const (
	plistBool plistKind = iota
	plistInt
	plistReal
	plistString
	plistData
)

// plistValue is one scalar of a property list.
type plistValue struct {
	kind plistKind
	b    bool
	i    int64
	f    float64
	s    string
	data []byte
}

func plistBoolean(v bool) plistValue              { return plistValue{kind: plistBool, b: v} }
func plistInteger(v int64) plistValue             { return plistValue{kind: plistInt, i: v} }
func plistFloat(v float64) plistValue             { return plistValue{kind: plistReal, f: v} }
func plistText(v string) plistValue               { return plistValue{kind: plistString, s: v} }
func plistBytes(v []byte) plistValue              { return plistValue{kind: plistData, data: v} }
func plistPair(k string, v plistValue) plistEntry { return plistEntry{key: k, value: v} }

type plistEntry struct {
	key   string
	value plistValue
}

// encodeBinaryPlist writes a bplist00 whose top object is a dictionary of the
// entries, in the order given. Object references are one byte, which bounds
// the dictionary to 127 entries; the Finder's own use is about twenty.
func encodeBinaryPlist(entries []plistEntry) ([]byte, error) {
	if len(entries) > 127 {
		return nil, errors.New("dmg: property list has too many entries")
	}
	out := []byte("bplist00")
	offsets := make([]int, 0, 1+2*len(entries))
	offsets = append(offsets, len(out))
	// The dictionary is object 0, its keys 1..n and its values n+1..2n.
	out = appendCount(out, 0xd0, len(entries))
	for index := range entries {
		out = append(out, byte(1+index))
	}
	for index := range entries {
		out = append(out, byte(1+len(entries)+index))
	}
	for _, entry := range entries {
		offsets = append(offsets, len(out))
		out = appendString(out, entry.key)
	}
	for _, entry := range entries {
		offsets = append(offsets, len(out))
		out = appendValue(out, entry.value)
	}
	table := len(out)
	offsetSize := 1
	if table > 0xffff {
		offsetSize = 4
	} else if table > 0xff {
		offsetSize = 2
	}
	for _, offset := range offsets {
		switch offsetSize {
		case 1:
			out = append(out, byte(offset))
		case 2:
			out = binary.BigEndian.AppendUint16(out, uint16(offset))
		default:
			out = binary.BigEndian.AppendUint32(out, uint32(offset))
		}
	}
	out = append(out, 0, 0, 0, 0, 0, 0, byte(offsetSize), 1)
	out = binary.BigEndian.AppendUint64(out, uint64(len(offsets)))
	out = binary.BigEndian.AppendUint64(out, 0)
	out = binary.BigEndian.AppendUint64(out, uint64(table))
	return out, nil
}

// appendCount writes a marker byte whose low nibble is the length, with the
// length as a following integer object when it does not fit in the nibble.
func appendCount(out []byte, marker byte, count int) []byte {
	if count < 15 {
		return append(out, marker|byte(count))
	}
	out = append(out, marker|0x0f)
	return appendInt(out, int64(count))
}

func appendInt(out []byte, v int64) []byte {
	switch {
	case v >= 0 && v <= 0xff:
		return append(out, 0x10, byte(v))
	case v >= 0 && v <= 0xffff:
		return binary.BigEndian.AppendUint16(append(out, 0x11), uint16(v))
	case v >= 0 && v <= math.MaxUint32:
		return binary.BigEndian.AppendUint32(append(out, 0x12), uint32(v))
	default:
		return binary.BigEndian.AppendUint64(append(out, 0x13), uint64(v))
	}
}

func appendString(out []byte, s string) []byte {
	ascii := true
	for index := 0; index < len(s); index++ {
		if s[index] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		out = appendCount(out, 0x50, len(s))
		return append(out, s...)
	}
	units := utf16.Encode([]rune(s))
	out = appendCount(out, 0x60, len(units))
	for _, unit := range units {
		out = binary.BigEndian.AppendUint16(out, unit)
	}
	return out
}

func appendValue(out []byte, v plistValue) []byte {
	switch v.kind {
	case plistBool:
		if v.b {
			return append(out, 0x09)
		}
		return append(out, 0x08)
	case plistInt:
		return appendInt(out, v.i)
	case plistReal:
		return binary.BigEndian.AppendUint64(append(out, 0x23), math.Float64bits(v.f))
	case plistString:
		return appendString(out, v.s)
	default:
		out = appendCount(out, 0x40, len(v.data))
		return append(out, v.data...)
	}
}
