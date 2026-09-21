package parserails

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Compound File Binary (CFB) is the container behind every pre-XML Microsoft
// format — .doc, .xls, .ppt, .msg — and behind the OLE objects embedded in
// modern ones. It is a FAT filesystem in a file: a header, a sector allocation
// table, a directory tree, and a second, smaller allocation table for streams
// below a cutoff.
//
// ParseRails reads it properly rather than scanning the bytes for a magic
// number, because scanning finds whatever fragment happens to look like a file
// and misses everything that is stored below the cutoff or fragmented across
// sectors.

const (
	cfbHeaderSize    = 512
	cfbDirEntrySize  = 128
	cfbEndOfChain    = 0xFFFFFFFE
	cfbFreeSector    = 0xFFFFFFFF
	cfbMaxSectorHops = 1 << 20 // a chain longer than this is a loop or a lie
)

// CFB directory entry types.
const (
	cfbTypeStorage = 1
	cfbTypeStream  = 2
	cfbTypeRoot    = 5
)

// cfbFile is a parsed Compound File.
type cfbFile struct {
	data        []byte
	sectorSize  int
	miniSize    int
	miniCutoff  uint32
	fat         []uint32
	miniFAT     []uint32
	entries     []cfbEntry
	miniStream  []byte
	rootEntryID int
}

// cfbEntry is one directory entry: a storage (directory) or a stream (file).
type cfbEntry struct {
	Name  string
	Type  byte
	Start uint32
	Size  uint64
	// Tree links, as stored: siblings in a red-black tree, plus the first
	// child of a storage.
	Left, Right, Child uint32
}

// openCFB parses a Compound File's header, allocation tables and directory.
func openCFB(data []byte) (*cfbFile, error) {
	if len(data) < cfbHeaderSize || string(data[:8]) != string(oleMagic) {
		return nil, fmt.Errorf("parserails: not a compound file")
	}
	f := &cfbFile{data: data}
	f.sectorSize = 1 << binary.LittleEndian.Uint16(data[30:32])
	f.miniSize = 1 << binary.LittleEndian.Uint16(data[32:34])
	f.miniCutoff = binary.LittleEndian.Uint32(data[56:60])
	if f.sectorSize < 128 || f.sectorSize > 1<<20 || f.miniSize < 16 {
		return nil, fmt.Errorf("parserails: compound file has an implausible sector size")
	}

	if err := f.readFAT(); err != nil {
		return nil, err
	}
	if err := f.readDirectory(binary.LittleEndian.Uint32(data[48:52])); err != nil {
		return nil, err
	}
	f.readMiniFAT(
		binary.LittleEndian.Uint32(data[60:64]),
		binary.LittleEndian.Uint32(data[64:68]),
	)
	return f, nil
}

// sector returns one sector's bytes, or nil when it is out of range.
func (f *cfbFile) sector(id uint32) []byte {
	if id >= cfbEndOfChain {
		return nil
	}
	start := (int(id) + 1) * f.sectorSize
	if start < 0 || start+f.sectorSize > len(f.data) {
		return nil
	}
	return f.data[start : start+f.sectorSize]
}

// sectorCount is how many sectors the file can actually hold. Header fields
// are untrusted: every count and size is checked against it before it becomes
// an allocation.
func (f *cfbFile) sectorCount() int { return len(f.data) / f.sectorSize }

// readFAT assembles the sector allocation table from the DIFAT.
func (f *cfbFile) readFAT() error {
	// A file cannot hold more FAT sectors than it holds sectors; a header
	// claiming four billion of them is a 16 GiB allocation, not a document.
	fatCount := min(int64(binary.LittleEndian.Uint32(f.data[44:48])), int64(f.sectorCount()))
	difat := make([]uint32, 0, fatCount)
	for i := 0; i < 109; i++ {
		off := 76 + i*4
		id := binary.LittleEndian.Uint32(f.data[off : off+4])
		if id >= cfbEndOfChain {
			continue
		}
		difat = append(difat, id)
	}
	// Files with more than 109 FAT sectors continue the DIFAT in its own
	// chain, each sector holding entries plus a pointer to the next. The
	// chain is followed with a visited set: a sector whose "next" points at
	// itself would otherwise grow the table forever out of a 4 KiB file.
	next := binary.LittleEndian.Uint32(f.data[68:72])
	seen := make(map[uint32]bool)
	for hops := 0; next < cfbEndOfChain && hops < f.sectorCount()+1; hops++ {
		if seen[next] {
			break
		}
		seen[next] = true
		sec := f.sector(next)
		if sec == nil {
			break
		}
		for i := 0; i+4 <= len(sec)-4; i += 4 {
			if id := binary.LittleEndian.Uint32(sec[i : i+4]); id < cfbEndOfChain {
				difat = append(difat, id)
			}
		}
		next = binary.LittleEndian.Uint32(sec[len(sec)-4:])
	}

	for _, id := range difat {
		sec := f.sector(id)
		if sec == nil {
			continue
		}
		for i := 0; i+4 <= len(sec); i += 4 {
			f.fat = append(f.fat, binary.LittleEndian.Uint32(sec[i:i+4]))
		}
	}
	if len(f.fat) == 0 {
		return fmt.Errorf("parserails: compound file has no allocation table")
	}
	return nil
}

func (f *cfbFile) readMiniFAT(start, count uint32) {
	if count == 0 {
		return
	}
	for _, sec := range f.chainSectors(start) {
		for i := 0; i+4 <= len(sec); i += 4 {
			f.miniFAT = append(f.miniFAT, binary.LittleEndian.Uint32(sec[i:i+4]))
		}
	}
	if f.rootEntryID >= 0 && f.rootEntryID < len(f.entries) {
		root := f.entries[f.rootEntryID]
		f.miniStream = f.readChain(root.Start, root.Size)
	}
}

func (f *cfbFile) readDirectory(start uint32) error {
	f.rootEntryID = -1
	for _, sec := range f.chainSectors(start) {
		for off := 0; off+cfbDirEntrySize <= len(sec); off += cfbDirEntrySize {
			raw := sec[off : off+cfbDirEntrySize]
			entry := cfbEntry{
				Name:  utf16Name(raw),
				Type:  raw[66],
				Left:  binary.LittleEndian.Uint32(raw[68:72]),
				Right: binary.LittleEndian.Uint32(raw[72:76]),
				Child: binary.LittleEndian.Uint32(raw[76:80]),
				Start: binary.LittleEndian.Uint32(raw[116:120]),
				Size:  binary.LittleEndian.Uint64(raw[120:128]),
			}
			if entry.Type == cfbTypeRoot && f.rootEntryID < 0 {
				f.rootEntryID = len(f.entries)
			}
			f.entries = append(f.entries, entry)
		}
	}
	if len(f.entries) == 0 {
		return fmt.Errorf("parserails: compound file has no directory")
	}
	return nil
}

// chainSectors walks a FAT chain and returns the sectors it covers.
func (f *cfbFile) chainSectors(start uint32) [][]byte {
	var (
		out  [][]byte
		seen = make(map[uint32]bool)
	)
	for id := start; id < cfbEndOfChain && len(out) < cfbMaxSectorHops; {
		if seen[id] {
			break // the chain loops back on itself
		}
		seen[id] = true
		sec := f.sector(id)
		if sec == nil {
			break
		}
		out = append(out, sec)
		if int(id) >= len(f.fat) {
			break
		}
		id = f.fat[id]
	}
	return out
}

// boundedSize caps a size taken from the file against the file itself: no
// stream inside a container can be larger than the container. Without it a
// forged directory entry (UINT64_MAX) panics the process in make().
func (f *cfbFile) boundedSize(size uint64) uint64 {
	return min(size, uint64(len(f.data)))
}

// readChain reads size bytes starting at a sector, following the FAT.
func (f *cfbFile) readChain(start uint32, size uint64) []byte {
	out := make([]byte, 0, f.boundedSize(size))
	for _, sec := range f.chainSectors(start) {
		out = append(out, sec...)
		if uint64(len(out)) >= size {
			break
		}
	}
	if uint64(len(out)) > size {
		out = out[:size]
	}
	return out
}

// readStream returns a directory entry's contents, from the mini stream when
// it is small enough to live there.
func (f *cfbFile) readStream(e cfbEntry) []byte {
	if e.Type != cfbTypeStream || e.Size == 0 {
		return nil
	}
	if e.Size >= uint64(f.miniCutoff) {
		return f.readChain(e.Start, e.Size)
	}

	out := make([]byte, 0, f.boundedSize(e.Size))
	seen := make(map[uint32]bool)
	for id := e.Start; id < cfbEndOfChain && uint64(len(out)) < e.Size; {
		if seen[id] {
			break
		}
		seen[id] = true
		from := int(id) * f.miniSize
		if from < 0 || from+f.miniSize > len(f.miniStream) {
			break
		}
		out = append(out, f.miniStream[from:from+f.miniSize]...)
		if int(id) >= len(f.miniFAT) {
			break
		}
		id = f.miniFAT[id]
	}
	if uint64(len(out)) > e.Size {
		out = out[:e.Size]
	}
	return out
}

// streams lists every stream in the file with its path, walking the directory
// tree from the root.
func (f *cfbFile) streams() []cfbStream {
	var out []cfbStream
	if f.rootEntryID < 0 {
		return nil
	}
	visited := make(map[uint32]bool)
	var walk func(id uint32, prefix string)
	walk = func(id uint32, prefix string) {
		// Compared in uint64 so a corrupt 32-bit id cannot wrap into range.
		if uint64(id) >= uint64(len(f.entries)) || visited[id] {
			return
		}
		visited[id] = true
		e := f.entries[id]
		path := e.Name
		if prefix != "" {
			path = prefix + "/" + e.Name
		}
		switch e.Type {
		case cfbTypeStream:
			out = append(out, cfbStream{Path: path, Entry: e})
		case cfbTypeStorage:
			walk(e.Child, path)
		}
		// Siblings live in a tree beside this entry, at the same level.
		walk(e.Left, prefix)
		walk(e.Right, prefix)
	}
	walk(f.entries[f.rootEntryID].Child, "")
	return out
}

// cfbStream is a stream with its path inside the compound file.
type cfbStream struct {
	Path  string
	Entry cfbEntry
}

// utf16Name decodes a directory entry's UTF-16 name.
func utf16Name(raw []byte) string {
	n := int(binary.LittleEndian.Uint16(raw[64:66]))
	if n < 2 || n > 64 {
		return ""
	}
	units := make([]uint16, 0, n/2)
	for i := 0; i+1 < n-1; i += 2 { // the stored length includes the NUL
		units = append(units, binary.LittleEndian.Uint16(raw[i:i+2]))
	}
	return strings.TrimRight(string(utf16.Decode(units)), "\x00")
}
