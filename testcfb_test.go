package parserails

import (
	"encoding/binary"
	"unicode/utf16"
)

// A minimal Compound File writer, so the CFB reader, the OLE container and the
// Outlook .msg reader can be tested against real structures rather than
// against checked-in binaries.

type cfbTestEntry struct {
	Name     string
	Data     []byte         // a stream
	Children []cfbTestEntry // a storage
}

const (
	cfbTestSectorSize = 512
	cfbTestMiniSize   = 64
	cfbTestMiniCutoff = 4096
	cfbFATSect        = 0xFFFFFFFD
	cfbNoStream       = 0xFFFFFFFF
)

// buildCFB writes a compound file holding the given tree. Streams go in the
// mini stream, which is where real files put anything small.
func buildCFB(entries []cfbTestEntry) []byte {
	type dirEntry struct {
		name               string
		kind               byte
		start              uint32
		size               uint64
		left, right, child uint32
	}

	dir := []dirEntry{{name: "Root Entry", kind: cfbTypeRoot, left: cfbNoStream, right: cfbNoStream, child: cfbNoStream}}
	var mini []byte

	// addStream copies data into the mini stream and returns its first mini
	// sector.
	addStream := func(data []byte) uint32 {
		first := uint32(len(mini) / cfbTestMiniSize)
		mini = append(mini, data...)
		for len(mini)%cfbTestMiniSize != 0 {
			mini = append(mini, 0)
		}
		return first
	}

	// add appends a subtree and returns the index of its first entry, with
	// siblings chained through Right.
	var add func(list []cfbTestEntry) uint32
	add = func(list []cfbTestEntry) uint32 {
		if len(list) == 0 {
			return cfbNoStream
		}
		ids := make([]int, 0, len(list))
		for _, e := range list {
			entry := dirEntry{name: e.Name, left: cfbNoStream, right: cfbNoStream, child: cfbNoStream}
			if e.Children != nil {
				entry.kind = cfbTypeStorage
			} else {
				entry.kind = cfbTypeStream
				entry.start = addStream(e.Data)
				entry.size = uint64(len(e.Data))
			}
			dir = append(dir, entry)
			ids = append(ids, len(dir)-1)
		}
		for i, e := range list {
			if e.Children != nil {
				dir[ids[i]].child = add(e.Children)
			}
			if i+1 < len(ids) {
				dir[ids[i]].right = uint32(ids[i+1])
			}
		}
		return uint32(ids[0])
	}
	dir[0].child = add(entries)

	// Lay out the sectors: directory, mini FAT, then the mini stream itself.
	dirSectors := (len(dir)*cfbDirEntrySize + cfbTestSectorSize - 1) / cfbTestSectorSize
	miniCount := len(mini) / cfbTestMiniSize
	miniFATBytes := ((miniCount*4 + cfbTestSectorSize - 1) / cfbTestSectorSize) * cfbTestSectorSize
	miniFATSectors := max(miniFATBytes/cfbTestSectorSize, 1)
	miniStreamSectors := (len(mini) + cfbTestSectorSize - 1) / cfbTestSectorSize

	dirStart := uint32(1)
	miniFATStart := dirStart + uint32(dirSectors)
	miniStreamStart := miniFATStart + uint32(miniFATSectors)
	total := int(miniStreamStart) + miniStreamSectors

	dir[0].start = miniStreamStart
	dir[0].size = uint64(len(mini))

	// FAT: sector 0 is the FAT itself, then each run is chained in order.
	fat := make([]uint32, cfbTestSectorSize/4)
	for i := range fat {
		fat[i] = cfbFreeSector
	}
	fat[0] = cfbFATSect
	chain := func(start, count int) {
		for i := 0; i < count; i++ {
			if i == count-1 {
				fat[start+i] = cfbEndOfChain
			} else {
				fat[start+i] = uint32(start + i + 1)
			}
		}
	}
	chain(int(dirStart), dirSectors)
	chain(int(miniFATStart), miniFATSectors)
	chain(int(miniStreamStart), miniStreamSectors)

	// Mini FAT: every stream was written as a run of consecutive mini sectors,
	// so each one chains forward until the next stream starts.
	miniFAT := make([]uint32, miniCount)
	for i := range miniFAT {
		miniFAT[i] = cfbEndOfChain
	}
	for _, e := range dir {
		if e.kind != cfbTypeStream || e.size == 0 {
			continue
		}
		used := int((e.size + cfbTestMiniSize - 1) / cfbTestMiniSize)
		for i := 0; i < used-1; i++ {
			miniFAT[int(e.start)+i] = e.start + uint32(i) + 1
		}
		miniFAT[int(e.start)+used-1] = cfbEndOfChain
	}

	out := make([]byte, cfbHeaderSize+total*cfbTestSectorSize)
	copy(out, oleMagic)
	binary.LittleEndian.PutUint16(out[30:32], 9) // 2^9 = 512-byte sectors
	binary.LittleEndian.PutUint16(out[32:34], 6) // 2^6 = 64-byte mini sectors
	binary.LittleEndian.PutUint32(out[44:48], 1) // one FAT sector
	binary.LittleEndian.PutUint32(out[48:52], dirStart)
	binary.LittleEndian.PutUint32(out[56:60], cfbTestMiniCutoff)
	binary.LittleEndian.PutUint32(out[60:64], miniFATStart)
	binary.LittleEndian.PutUint32(out[64:68], uint32(miniFATSectors))
	binary.LittleEndian.PutUint32(out[68:72], cfbEndOfChain) // no extra DIFAT
	binary.LittleEndian.PutUint32(out[72:76], 0)
	for i := 0; i < 109; i++ {
		id := uint32(cfbFreeSector)
		if i == 0 {
			id = 0 // the FAT lives in sector 0
		}
		binary.LittleEndian.PutUint32(out[76+i*4:80+i*4], id)
	}

	sectorAt := func(id int) []byte {
		off := cfbHeaderSize + id*cfbTestSectorSize
		return out[off : off+cfbTestSectorSize]
	}
	for i, v := range fat {
		binary.LittleEndian.PutUint32(sectorAt(0)[i*4:i*4+4], v)
	}
	for i, e := range dir {
		raw := sectorAt(int(dirStart) + i*cfbDirEntrySize/cfbTestSectorSize)
		off := (i * cfbDirEntrySize) % cfbTestSectorSize
		writeCFBEntry(raw[off:off+cfbDirEntrySize], e.name, e.kind, e.start, e.size, e.left, e.right, e.child)
	}
	for i, v := range miniFAT {
		sector := sectorAt(int(miniFATStart) + (i*4)/cfbTestSectorSize)
		binary.LittleEndian.PutUint32(sector[(i*4)%cfbTestSectorSize:], v)
	}
	copy(out[cfbHeaderSize+int(miniStreamStart)*cfbTestSectorSize:], mini)
	return out
}

func writeCFBEntry(raw []byte, name string, kind byte, start uint32, size uint64, left, right, child uint32) {
	units := utf16.Encode([]rune(name))
	for i, u := range units {
		binary.LittleEndian.PutUint16(raw[i*2:i*2+2], u)
	}
	binary.LittleEndian.PutUint16(raw[64:66], uint16(len(units)*2+2)) // includes the NUL
	raw[66] = kind
	binary.LittleEndian.PutUint32(raw[68:72], left)
	binary.LittleEndian.PutUint32(raw[72:76], right)
	binary.LittleEndian.PutUint32(raw[76:80], child)
	binary.LittleEndian.PutUint32(raw[116:120], start)
	binary.LittleEndian.PutUint64(raw[120:128], size)
}

// utf16Bytes encodes a string the way a MAPI PT_UNICODE property stores it.
func utf16Bytes(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(out[i*2:i*2+2], u)
	}
	return out
}
