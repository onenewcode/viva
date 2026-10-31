package dmg

import (
	"encoding/binary"
	"errors"
	"time"
	"unicode/utf16"
)

// macEpoch is where the classic Mac OS counts seconds from.
var macEpoch = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)

// File is a file at the top of a mounted volume, as the Finder's background
// picture setting refers to it: by a version 2 alias record, the format HFS+
// volumes have always used. The Finder resolves an alias by volume name,
// creation dates and catalog node IDs first and by path only as a fallback, so
// every field is the volume's own, read after it was mounted.
type File struct {
	// Volume is the volume's name, which is also its folder under /Volumes.
	Volume        string
	VolumeCreated time.Time
	// Name is the file's name at the top of the volume.
	Name    string
	Created time.Time
	// ID and ParentID are the catalog node IDs of the file and of the volume's
	// root folder (2 on HFS+).
	ID, ParentID uint32
}

// Alias encodes the file as an alias record.
func (f File) Alias() ([]byte, error) {
	if f.Volume == "" || f.Name == "" {
		return nil, errors.New("dmg: an alias needs a volume and a file name")
	}
	volumeDate := uint32(f.VolumeCreated.Sub(macEpoch) / time.Second)
	fileDate := uint32(f.Created.Sub(macEpoch) / time.Second)
	carbonVolume, carbonName := carbon(f.Volume), carbon(f.Name)

	out := make([]byte, 0, 256)
	out = append(out, 0, 0, 0, 0) // application info
	out = append(out, 0, 0)       // record size, set at the end
	out = binary.BigEndian.AppendUint16(out, 2)
	out = binary.BigEndian.AppendUint16(out, 0) // a file, not a folder
	out = appendPascal(out, carbonVolume, 28)
	out = binary.BigEndian.AppendUint32(out, volumeDate)
	out = append(out, 'H', '+')
	out = binary.BigEndian.AppendUint16(out, 0) // fixed disk
	out = binary.BigEndian.AppendUint32(out, f.ParentID)
	out = appendPascal(out, carbonName, 64)
	out = binary.BigEndian.AppendUint32(out, f.ID)
	out = binary.BigEndian.AppendUint32(out, fileDate)
	out = append(out, 0, 0, 0, 0, 0, 0, 0, 0)       // creator and type codes
	out = append(out, 0xff, 0xff, 0xff, 0xff)       // levels from and to: -1
	out = binary.BigEndian.AppendUint32(out, 0)     // volume attribute flags
	out = append(out, 0, 0)                         // file system ID
	out = append(out, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0) // reserved

	out = appendTag(out, 0, []byte(carbonVolume)) // the folder the file is in
	out = appendTag(out, 16, binary.BigEndian.AppendUint64(nil, uint64(volumeDate)<<16))
	out = appendTag(out, 17, binary.BigEndian.AppendUint64(nil, uint64(fileDate)<<16))
	out = appendTag(out, 2, []byte(carbonVolume+":"+carbonName))
	out = appendTag(out, 14, unicodeName(f.Name))
	out = appendTag(out, 15, unicodeName(f.Volume))
	out = appendTag(out, 18, []byte("/"+f.Name))
	out = appendTag(out, 19, []byte("/Volumes/"+f.Volume))
	out = append(out, 0xff, 0xff, 0, 0)

	binary.BigEndian.PutUint16(out[4:], uint16(len(out)))
	return out, nil
}

// carbon is a name as the old file manager writes it, where the colon is the
// path separator.
func carbon(name string) string {
	out := []byte(name)
	for index, c := range out {
		if c == ':' {
			out[index] = '/'
		}
	}
	return string(out)
}

// appendPascal writes a length-prefixed string padded to size bytes in all.
func appendPascal(out []byte, s string, size int) []byte {
	if len(s) > size-1 {
		s = s[:size-1]
	}
	out = append(out, byte(len(s)))
	out = append(out, s...)
	for pad := size - 1 - len(s); pad > 0; pad-- {
		out = append(out, 0)
	}
	return out
}

// appendTag writes a variable part, padded to an even length.
func appendTag(out []byte, tag int16, data []byte) []byte {
	out = binary.BigEndian.AppendUint16(out, uint16(tag))
	out = binary.BigEndian.AppendUint16(out, uint16(len(data)))
	out = append(out, data...)
	if len(data)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

// unicodeName is a name as a count of UTF-16 units and the units.
func unicodeName(name string) []byte {
	units := utf16.Encode([]rune(name))
	out := binary.BigEndian.AppendUint16(nil, uint16(len(units)))
	for _, unit := range units {
		out = binary.BigEndian.AppendUint16(out, unit)
	}
	return out
}
