package infrastructure

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	mp4 "github.com/abema/go-mp4"
)

func writeThreeFrameMP4(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "three.m4a")
	writer, err := NewMP4AudioWriter(path, MP4AudioTrack{AudioSpecificConfig: []byte{0x12, 0x10}, Channels: 2, Bitrate: 256000, EncoderDelay: 2048})
	if err != nil {
		t.Fatal(err)
	}
	for index := range 3 {
		if err := writer.WriteFrame(bytes.Repeat([]byte{byte(index + 1)}, 10)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Finish(1024); err != nil {
		t.Fatal(err)
	}
	return path
}

func boxLayoutOf(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	var layout strings.Builder
	_, err = mp4.ReadBoxStructure(file, func(handle *mp4.ReadHandle) (any, error) {
		layout.WriteString(strings.Repeat(" ", len(handle.Path)-1) + handle.BoxInfo.Type.String() + " " + strconv.FormatUint(handle.BoxInfo.Size, 10) + "\n")
		if handle.BoxInfo.IsSupportedType() && handle.BoxInfo.Type != mp4.BoxTypeMdat() {
			return handle.Expand()
		}
		return handle.BoxInfo, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return layout.String()
}

func TestMP4AudioWriter_BoxLayoutGolden(t *testing.T) {
	path := writeThreeFrameMP4(t)

	layout := boxLayoutOf(t, path)

	golden := `ftyp 28
moov 612
 mvhd 108
 trak 496
  tkhd 92
  edts 36
   elst 28
  mdia 360
   mdhd 32
   hdlr 45
   minf 275
    smhd 16
    dinf 36
     dref 28
      url  12
    stbl 215
     stsd 103
      mp4a 87
       esds 51
     stts 24
     stsc 28
     stsz 32
     stco 20
mdat 38
`
	if layout != golden {
		t.Fatalf("layout:\n%s", layout)
	}
}

func TestMP4AudioWriter_ChunkOffsetPointsAtFirstFrame(t *testing.T) {
	path := writeThreeFrameMP4(t)

	content, err := os.ReadFile(filepath.Clean(path))

	chunkOffset := 28 + 612 + 8
	if err != nil || !bytes.Equal(content[chunkOffset:chunkOffset+10], bytes.Repeat([]byte{1}, 10)) || !bytes.Contains(content, []byte{'s', 't', 'c', 'o', 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 2, 0x88}) {
		t.Fatalf("first frame not at chunk offset %d err = %v", chunkOffset, err)
	}
}

func TestMP4AudioWriter_Errors(t *testing.T) {
	_, createErr := NewMP4AudioWriter(filepath.Join(t.TempDir(), "missing", "x.m4a"), MP4AudioTrack{})
	writer, err := NewMP4AudioWriter(filepath.Join(t.TempDir(), "x.m4a"), MP4AudioTrack{})
	if err != nil {
		t.Fatal(err)
	}
	abortErr := writer.Abort()
	writeErr := writer.WriteFrame([]byte{1})
	finishErr := writer.Finish(1)

	if !errors.Is(createErr, os.ErrNotExist) || abortErr != nil || writeErr == nil || finishErr == nil {
		t.Fatalf("errors = %v %v %v %v", createErr, abortErr, writeErr, finishErr)
	}
}

func TestMemoryFile_Seek(t *testing.T) {
	file := &memoryFile{}
	_, _ = file.Write([]byte("abcdef"))

	start, startErr := file.Seek(2, 0)
	end, endErr := file.Seek(-1, 2)
	_, invalidErr := file.Seek(-10, 1)
	_, whenceErr := file.Seek(0, 9)

	if start != 2 || end != 5 || startErr != nil || endErr != nil || invalidErr == nil || whenceErr == nil {
		t.Fatalf("start = %d end = %d errors = %v %v %v %v", start, end, startErr, endErr, invalidErr, whenceErr)
	}
}

func TestMdatHeader_LargePayloadUsesLargeSize(t *testing.T) {
	small := mdatHeader(10)
	large := mdatHeader(1 << 32)

	if !bytes.Equal(small, []byte{0, 0, 0, 18, 'm', 'd', 'a', 't'}) || len(large) != 16 || !bytes.Equal(large[:8], []byte{0, 0, 0, 1, 'm', 'd', 'a', 't'}) || large[11] != 1 || large[15] != 16 {
		t.Fatalf("small = % x large = % x", small, large)
	}
}
