package infrastructure

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	mp4 "github.com/abema/go-mp4"
)

const (
	mp4Timescale          = 44100
	mp4SamplesPerFrame    = 1024
	mp4TrackID            = 1
	mp4MdatHeaderSize     = 8
	mp4LargeMdatHeader    = 16
	mp4FixedPointOne      = 0x00010000
	mp4UnityVolume        = 0x0100
	mp4MatrixW            = 0x40000000
	mp4SampleBits         = 16
	mp4AudioObjectMPEG4   = 0x40
	mp4AudioStreamType    = 0x05
	mp4ESDescriptorTag    = 0x03
	mp4DecoderConfigTag   = 0x04
	mp4DecoderSpecificTag = 0x05
	mp4SLConfigTag        = 0x06
	mp4SLConfigPredefined = 0x02
	mp4DecoderConfigBytes = 13
	mp4ESDescriptorBytes  = 3
	mp4DescriptorHeader   = 5
	mp4URLSelfContained   = 0x01
	mp4MdatSuffix         = ".mdat.tmp"
	mp4ComposedSuffix     = ".tmp"
)

var mp4Matrix = [9]int32{mp4FixedPointOne, 0, 0, 0, mp4FixedPointOne, 0, 0, 0, mp4MatrixW}

type MP4AudioTrack struct {
	AudioSpecificConfig []byte
	Channels            int
	Bitrate             int
	EncoderDelay        int
}

type MP4AudioWriter struct {
	path        string
	track       MP4AudioTrack
	mdat        *os.File
	sampleSizes []uint32
	mdatBytes   uint64
}

func NewMP4AudioWriter(path string, track MP4AudioTrack) (*MP4AudioWriter, error) {
	mdat, err := os.Create(filepath.Clean(path + mp4MdatSuffix))
	if err != nil {
		return nil, fmt.Errorf("creating media data for %s: %w", path, err)
	}
	return &MP4AudioWriter{path: path, track: track, mdat: mdat, sampleSizes: []uint32{}}, nil
}

func (writer *MP4AudioWriter) WriteFrame(frame []byte) error {
	if _, err := writer.mdat.Write(frame); err != nil {
		return fmt.Errorf("writing audio frame: %w", err)
	}
	writer.sampleSizes = append(writer.sampleSizes, clampUint32(int64(len(frame))))
	writer.mdatBytes += uint64(len(frame))
	return nil
}

func (writer *MP4AudioWriter) Finish(sourceSamples int64) error {
	if _, err := writer.mdat.Seek(0, io.SeekStart); err != nil {
		return errors.Join(fmt.Errorf("rewinding media data: %w", err), writer.Abort())
	}
	if err := writer.compose(sourceSamples); err != nil {
		return errors.Join(err, writer.Abort())
	}
	if err := writer.Abort(); err != nil {
		return err
	}
	if err := os.Rename(writer.path+mp4ComposedSuffix, writer.path); err != nil {
		return fmt.Errorf("renaming %s: %w", writer.path, err)
	}
	return nil
}

func (writer *MP4AudioWriter) Abort() error {
	closeErr := writer.mdat.Close()
	if errors.Is(closeErr, os.ErrClosed) {
		closeErr = nil
	}
	removeErr := os.Remove(writer.path + mp4MdatSuffix)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}

func (writer *MP4AudioWriter) compose(sourceSamples int64) error {
	header, err := writer.header(sourceSamples)
	if err != nil {
		return err
	}
	output, err := os.Create(filepath.Clean(writer.path + mp4ComposedSuffix))
	if err != nil {
		return fmt.Errorf("creating %s: %w", writer.path, err)
	}
	_, writeErr := output.Write(header)
	if writeErr == nil {
		writeErr = writer.writeMdat(output)
	}
	if writeErr == nil {
		writeErr = output.Sync()
	}
	return errors.Join(writeErr, output.Close())
}

func (writer *MP4AudioWriter) writeMdat(output io.Writer) error {
	if _, err := output.Write(mdatHeader(writer.mdatBytes)); err != nil {
		return fmt.Errorf("writing media data header: %w", err)
	}
	if _, err := io.Copy(output, writer.mdat); err != nil {
		return fmt.Errorf("copying media data: %w", err)
	}
	return nil
}

func (writer *MP4AudioWriter) header(sourceSamples int64) ([]byte, error) {
	layout := mp4Layout{track: writer.track, sampleSizes: writer.sampleSizes, sourceSamples: sourceSamples}
	measured, err := layout.marshal()
	if err != nil {
		return nil, err
	}
	layout.chunkOffset = uint64(len(measured)) + uint64(len(mdatHeader(writer.mdatBytes)))
	layout.useLargeOffsets = layout.chunkOffset+writer.mdatBytes > math.MaxUint32
	if layout.useLargeOffsets {
		measured, err = layout.marshal()
		if err != nil {
			return nil, err
		}
		layout.chunkOffset = uint64(len(measured)) + uint64(len(mdatHeader(writer.mdatBytes)))
	}
	return layout.marshal()
}

func mdatHeader(payload uint64) []byte {
	if payload+mp4MdatHeaderSize > math.MaxUint32 {
		header := []byte{0, 0, 0, 1, 'm', 'd', 'a', 't', 0, 0, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint64(header[8:], payload+mp4LargeMdatHeader)
		return header
	}
	header := []byte{0, 0, 0, 0, 'm', 'd', 'a', 't'}
	binary.BigEndian.PutUint32(header, clampUint32(int64(payload+mp4MdatHeaderSize)))
	return header
}

func clampUint32(value int64) uint32 {
	if value < 0 {
		return 0
	}
	if value > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value)
}

func clampUint16(value int) uint16 {
	if value < 0 {
		return 0
	}
	if value > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(value)
}

type mp4Layout struct {
	track           MP4AudioTrack
	sampleSizes     []uint32
	sourceSamples   int64
	chunkOffset     uint64
	useLargeOffsets bool
}

func (layout mp4Layout) marshal() ([]byte, error) {
	buffer := &memoryFile{}
	composer := &boxComposer{writer: mp4.NewWriter(buffer)}
	composer.leaf(mp4.BoxTypeFtyp(), &mp4.Ftyp{
		MajorBrand: [4]byte{'M', '4', 'A', ' '}, MinorVersion: 0x200,
		CompatibleBrands: []mp4.CompatibleBrandElem{{CompatibleBrand: [4]byte{'M', '4', 'A', ' '}}, {CompatibleBrand: [4]byte{'m', 'p', '4', '2'}}, {CompatibleBrand: [4]byte{'i', 's', 'o', 'm'}}},
	})
	composer.start(mp4.BoxTypeMoov())
	composer.leaf(mp4.BoxTypeMvhd(), &mp4.Mvhd{Timescale: mp4Timescale, DurationV0: clampUint32(layout.sourceSamples), Rate: mp4FixedPointOne, Volume: mp4UnityVolume, Matrix: mp4Matrix, NextTrackID: mp4TrackID + 1})
	layout.writeTrack(composer)
	composer.end()
	if composer.err != nil {
		return nil, fmt.Errorf("composing mp4 header: %w", composer.err)
	}
	return buffer.data, nil
}

func (layout mp4Layout) writeTrack(composer *boxComposer) {
	composer.start(mp4.BoxTypeTrak())
	composer.leaf(mp4.BoxTypeTkhd(), &mp4.Tkhd{FullBox: mp4.FullBox{Flags: [3]byte{0, 0, 7}}, TrackID: mp4TrackID, DurationV0: clampUint32(layout.sourceSamples), Volume: mp4UnityVolume, Matrix: mp4Matrix})
	composer.start(mp4.BoxTypeEdts())
	composer.leaf(mp4.BoxTypeElst(), &mp4.Elst{EntryCount: 1, Entries: []mp4.ElstEntry{{SegmentDurationV0: clampUint32(layout.sourceSamples), MediaTimeV0: clampInt32(layout.track.EncoderDelay), MediaRateInteger: 1}}})
	composer.end()
	composer.start(mp4.BoxTypeMdia())
	composer.leaf(mp4.BoxTypeMdhd(), &mp4.Mdhd{Timescale: mp4Timescale, DurationV0: clampUint32(int64(len(layout.sampleSizes)) * mp4SamplesPerFrame), Language: [3]byte{'u' - 0x60, 'n' - 0x60, 'd' - 0x60}})
	composer.leaf(mp4.BoxTypeHdlr(), &mp4.Hdlr{HandlerType: [4]byte{'s', 'o', 'u', 'n'}, Name: "SoundHandler"})
	composer.start(mp4.BoxTypeMinf())
	composer.leaf(mp4.BoxTypeSmhd(), &mp4.Smhd{})
	writeDataInformation(composer)
	layout.writeSampleTable(composer)
	composer.end()
	composer.end()
	composer.end()
}

func writeDataInformation(composer *boxComposer) {
	composer.start(mp4.BoxTypeDinf())
	composer.start(mp4.BoxTypeDref())
	composer.marshal(&mp4.Dref{EntryCount: 1})
	composer.leaf(mp4.BoxTypeUrl(), &mp4.Url{FullBox: mp4.FullBox{Flags: [3]byte{0, 0, mp4URLSelfContained}}})
	composer.end()
	composer.end()
}

func (layout mp4Layout) writeSampleTable(composer *boxComposer) {
	sampleCount := clampUint32(int64(len(layout.sampleSizes)))
	composer.start(mp4.BoxTypeStbl())
	layout.writeSampleDescription(composer)
	composer.leaf(mp4.BoxTypeStts(), &mp4.Stts{EntryCount: 1, Entries: []mp4.SttsEntry{{SampleCount: sampleCount, SampleDelta: mp4SamplesPerFrame}}})
	composer.leaf(mp4.BoxTypeStsc(), &mp4.Stsc{EntryCount: 1, Entries: []mp4.StscEntry{{FirstChunk: 1, SamplesPerChunk: sampleCount, SampleDescriptionIndex: 1}}})
	composer.leaf(mp4.BoxTypeStsz(), &mp4.Stsz{SampleCount: sampleCount, EntrySize: layout.sampleSizes})
	if layout.useLargeOffsets {
		composer.leaf(mp4.BoxTypeCo64(), &mp4.Co64{EntryCount: 1, ChunkOffset: []uint64{layout.chunkOffset}})
	} else {
		composer.leaf(mp4.BoxTypeStco(), &mp4.Stco{EntryCount: 1, ChunkOffset: []uint32{clampUint32(int64(min(layout.chunkOffset, math.MaxUint32)))}})
	}
	composer.end()
}

func (layout mp4Layout) writeSampleDescription(composer *boxComposer) {
	composer.start(mp4.BoxTypeStsd())
	composer.marshal(&mp4.Stsd{EntryCount: 1})
	composer.start(mp4.BoxTypeMp4a())
	composer.marshal(&mp4.AudioSampleEntry{
		SampleEntry:  mp4.SampleEntry{AnyTypeBox: mp4.AnyTypeBox{Type: mp4.BoxTypeMp4a()}, DataReferenceIndex: 1},
		ChannelCount: clampUint16(layout.track.Channels), SampleSize: mp4SampleBits, SampleRate: mp4Timescale << 16,
	})
	composer.leaf(mp4.BoxTypeEsds(), &mp4.Esds{Descriptors: elementaryStreamDescriptors(layout.track)})
	composer.end()
	composer.end()
}

func elementaryStreamDescriptors(track MP4AudioTrack) []mp4.Descriptor {
	specificSize := clampUint32(int64(len(track.AudioSpecificConfig)))
	decoderSize := mp4DecoderConfigBytes + mp4DescriptorHeader + specificSize
	esSize := mp4ESDescriptorBytes + mp4DescriptorHeader + decoderSize + mp4DescriptorHeader + 1
	return []mp4.Descriptor{
		{Tag: mp4ESDescriptorTag, Size: esSize, ESDescriptor: &mp4.ESDescriptor{ESID: mp4TrackID}},
		{Tag: mp4DecoderConfigTag, Size: decoderSize, DecoderConfigDescriptor: &mp4.DecoderConfigDescriptor{
			ObjectTypeIndication: mp4AudioObjectMPEG4, StreamType: mp4AudioStreamType, Reserved: true,
			MaxBitrate: clampUint32(int64(track.Bitrate)), AvgBitrate: clampUint32(int64(track.Bitrate)),
		}},
		{Tag: mp4DecoderSpecificTag, Size: specificSize, Data: track.AudioSpecificConfig},
		{Tag: mp4SLConfigTag, Size: 1, Data: []byte{mp4SLConfigPredefined}},
	}
}

type boxComposer struct {
	writer *mp4.Writer
	err    error
}

func (composer *boxComposer) leaf(boxType mp4.BoxType, box mp4.IImmutableBox) {
	composer.start(boxType)
	composer.marshal(box)
	composer.end()
}

func (composer *boxComposer) start(boxType mp4.BoxType) {
	if composer.err == nil {
		_, composer.err = composer.writer.StartBox(&mp4.BoxInfo{Type: boxType})
	}
}

func (composer *boxComposer) marshal(box mp4.IImmutableBox) {
	if composer.err == nil {
		_, composer.err = mp4.Marshal(composer.writer, box, mp4.Context{})
	}
}

func (composer *boxComposer) end() {
	if composer.err == nil {
		_, composer.err = composer.writer.EndBox()
	}
}

type memoryFile struct {
	data     []byte
	position int64
}

func (file *memoryFile) Write(chunk []byte) (int, error) {
	end := file.position + int64(len(chunk))
	if end > int64(len(file.data)) {
		file.data = append(file.data, make([]byte, end-int64(len(file.data)))...)
	}
	copy(file.data[file.position:end], chunk)
	file.position = end
	return len(chunk), nil
}

func (file *memoryFile) Seek(offset int64, whence int) (int64, error) {
	origins := map[int]int64{io.SeekStart: 0, io.SeekCurrent: file.position, io.SeekEnd: int64(len(file.data))}
	origin, isKnown := origins[whence]
	if !isKnown || origin+offset < 0 {
		return 0, fmt.Errorf("invalid seek %d from %d", offset, whence)
	}
	file.position = origin + offset
	return file.position, nil
}
