package dmg

import (
	"encoding/binary"
	"errors"
	"sort"
	"strings"
	"unicode/utf16"
)

// A .DS_Store file is where the Finder keeps how it shows a folder: the size of
// its window, the background, where each icon sits. It is a B-tree of records
// stored in blocks of a "buddy" allocator. Apple does not document the format;
// this follows Wim Lewis's notes and the files the Finder writes, and writes
// the smallest valid one: a single leaf node, which holds about two kilobytes
// of records, plenty for a disk image with two icons.

// dsRecord is one entry of the tree: a structure of a file or folder, named by
// a four-character code and typed by another.
type dsRecord struct {
	name string // the file, or "." for the folder itself
	code string // for example "Iloc" or "bwsp"
	kind string // "long", "blob" or "type"
	data []byte // the value as written, without its length for a blob
}

func dsLong(name, code string, v uint32) dsRecord {
	return dsRecord{name, code, "long", binary.BigEndian.AppendUint32(nil, v)}
}

func dsType(name, code, v string) dsRecord { return dsRecord{name, code, "type", []byte(v)} }

func dsBlob(name, code string, v []byte) dsRecord { return dsRecord{name, code, "blob", v} }

// encode is the record as it is stored in a node.
func (r dsRecord) encode() []byte {
	units := utf16.Encode([]rune(r.name))
	out := binary.BigEndian.AppendUint32(nil, uint32(len(units)))
	for _, unit := range units {
		out = binary.BigEndian.AppendUint16(out, unit)
	}
	out = append(out, r.code...)
	out = append(out, r.kind...)
	if r.kind == "blob" {
		out = binary.BigEndian.AppendUint32(out, uint32(len(r.data)))
	}
	return append(out, r.data...)
}

const (
	// rootWidth is the size, as a power of two, of the allocator's root block:
	// its table of block addresses is padded to 256 entries, a little over 1 KiB.
	rootWidth = 11
	// pageSize is the node size the tree declares.
	pageSize = 4096
)

// encodeDSStore lays the records out as a complete .DS_Store file.
func encodeDSStore(records []dsRecord) ([]byte, error) {
	sorted := append([]dsRecord(nil), records...)
	// The Finder orders names without regard to case, then structures by code.
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := strings.ToLower(sorted[i].name), strings.ToLower(sorted[j].name)
		if a != b {
			return a < b
		}
		return sorted[i].code < sorted[j].code
	})
	node := binary.BigEndian.AppendUint32(nil, 0) // a leaf has no right-most child
	node = binary.BigEndian.AppendUint32(node, uint32(len(sorted)))
	for _, record := range sorted {
		node = append(node, record.encode()...)
	}
	if len(node) > pageSize {
		return nil, errors.New("dmg: the folder's settings do not fit in one .DS_Store page")
	}
	nodeWidth := 5
	for 1<<nodeWidth < len(node) {
		nodeWidth++
	}

	// The free lists of an allocator that has just handed out the header and the
	// root block: one free block of each width, at the offset of that width.
	var free [32][]uint32
	for width := 5; width < 31; width++ {
		if width != rootWidth {
			free[width] = []uint32{1 << width}
		}
	}
	// alloc hands out the lowest block of the width, splitting a larger one and
	// leaving its upper half free.
	alloc := func(width int) uint32 {
		have := width
		for len(free[have]) == 0 {
			have++
		}
		offset := free[have][0]
		free[have] = free[have][1:]
		for have > width {
			have--
			free[have] = append(free[have], offset^(1<<have))
		}
		return offset
	}
	rootOffset := uint32(1 << rootWidth)
	superOffset := alloc(5)
	nodeOffset := alloc(nodeWidth)
	addresses := []uint32{rootOffset | rootWidth, superOffset | 5, nodeOffset | uint32(nodeWidth)}

	// Block 0 is the allocator's own table of contents.
	root := binary.BigEndian.AppendUint32(nil, uint32(len(addresses)))
	root = binary.BigEndian.AppendUint32(root, 0)
	for index := 0; index < 256; index++ {
		var address uint32
		if index < len(addresses) {
			address = addresses[index]
		}
		root = binary.BigEndian.AppendUint32(root, address)
	}
	root = binary.BigEndian.AppendUint32(root, 1)
	root = append(root, byte(len("DSDB")))
	root = append(root, "DSDB"...)
	root = binary.BigEndian.AppendUint32(root, 1) // block 1 holds the tree's header
	for _, list := range free {
		root = binary.BigEndian.AppendUint32(root, uint32(len(list)))
		for _, offset := range list {
			root = binary.BigEndian.AppendUint32(root, offset)
		}
	}
	if len(root) > 1<<rootWidth {
		return nil, errors.New("dmg: the allocator's table does not fit its block")
	}

	// Block 1: the root node is block 2, the tree has one level, n records, one
	// node.
	super := binary.BigEndian.AppendUint32(nil, 2)
	super = binary.BigEndian.AppendUint32(super, 0)
	super = binary.BigEndian.AppendUint32(super, uint32(len(sorted)))
	super = binary.BigEndian.AppendUint32(super, 1)
	super = binary.BigEndian.AppendUint32(super, pageSize)

	end := int(nodeOffset) + 1<<nodeWidth
	if rootEnd := int(rootOffset) + 1<<rootWidth; rootEnd > end {
		end = rootEnd
	}
	file := make([]byte, 4+end)
	binary.BigEndian.PutUint32(file, 1)
	copy(file[4:], "Bud1")
	binary.BigEndian.PutUint32(file[8:], rootOffset)
	binary.BigEndian.PutUint32(file[12:], 1<<rootWidth)
	binary.BigEndian.PutUint32(file[16:], rootOffset)
	copy(file[20:], []byte{0, 0, 0x10, 0x0c, 0, 0, 0, 0x87, 0, 0, 0x20, 0x0b, 0, 0, 0, 0})
	copy(file[4+rootOffset:], root)
	copy(file[4+superOffset:], super)
	copy(file[4+nodeOffset:], node)
	return file, nil
}
