package parserails

import (
	"context"
	"encoding/binary"
	"testing"
	"time"
)

// The header, the DIFAT chain and every directory entry are attacker
// controlled. None of them may become an allocation or a loop.

func TestCFBRejectsForgedStreamSize(t *testing.T) {
	data := buildCFB([]cfbTestEntry{{Name: "payload", Data: []byte("x")}})
	// The first stream entry follows the root entry in the directory sector.
	off := cfbHeaderSize + cfbTestSectorSize + cfbDirEntrySize + 120
	binary.LittleEndian.PutUint64(data[off:off+8], ^uint64(0))

	children, err := (oleContainer{}).Children(context.Background(), data, ChildRequest{})
	if err != nil {
		t.Logf("error (acceptable): %v", err)
	}
	t.Logf("children=%d", len(children)) // the point is that it did not panic
}

func TestCFBRejectsForgedFATCount(t *testing.T) {
	data := buildCFB([]cfbTestEntry{{Name: "payload", Data: []byte("x")}})
	binary.LittleEndian.PutUint32(data[44:48], 0xFFFFFFFF) // 4 billion FAT sectors

	if _, err := openCFB(data); err != nil {
		t.Logf("error (acceptable): %v", err)
	}
}

func TestCFBDoesNotAmplifyTheAllocationTable(t *testing.T) {
	// A DIFAT chain of sectors filled with one repeated valid id: each
	// appearance used to cost a whole sector of FAT entries, turning a
	// megabyte of input into gigabytes of table.
	const sectorSize = 4096
	data := make([]byte, cfbHeaderSize+sectorSize*210)
	copy(data, oleMagic)
	binary.LittleEndian.PutUint16(data[30:32], 12)
	binary.LittleEndian.PutUint16(data[32:34], 6)
	binary.LittleEndian.PutUint32(data[44:48], 1)
	binary.LittleEndian.PutUint32(data[48:52], 1)
	binary.LittleEndian.PutUint32(data[56:60], 4096)
	binary.LittleEndian.PutUint32(data[68:72], 2)
	for i := 0; i < 109; i++ {
		binary.LittleEndian.PutUint32(data[76+i*4:80+i*4], cfbFreeSector)
	}
	for s := 2; s <= 201; s++ {
		off := cfbHeaderSize + s*sectorSize
		next := uint32(s + 1)
		if s == 201 {
			next = cfbEndOfChain
		}
		binary.LittleEndian.PutUint32(data[off+sectorSize-4:off+sectorSize], next)
	}

	f, err := openCFB(data)
	if err != nil {
		return // refusing it outright is fine too
	}
	if len(f.fat) > f.sectorCount()+1 {
		t.Fatalf("allocation table has %d entries for a %d sector file", len(f.fat), f.sectorCount())
	}
}

func TestCFBSurvivesSelfReferentialDIFAT(t *testing.T) {
	data := buildCFB([]cfbTestEntry{{Name: "payload", Data: []byte("x")}})
	// Point the DIFAT chain at sector 0, whose trailing "next" pointer is part
	// of the FAT it holds — a sector that can point back at itself.
	binary.LittleEndian.PutUint32(data[68:72], 0)
	tail := cfbHeaderSize + cfbTestSectorSize - 4
	binary.LittleEndian.PutUint32(data[tail:tail+4], 0)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = openCFB(data)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("openCFB did not terminate on a self-referential DIFAT chain")
	}
}
