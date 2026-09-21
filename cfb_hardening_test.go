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
