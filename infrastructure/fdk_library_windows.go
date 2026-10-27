//go:build windows

package infrastructure

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	aacEncoderParameterAOT           = 0x0100
	aacEncoderParameterBitrate       = 0x0101
	aacEncoderParameterBitrateMode   = 0x0102
	aacEncoderParameterSampleRate    = 0x0103
	aacEncoderParameterChannelMode   = 0x0106
	aacEncoderParameterAfterburner   = 0x0200
	aacEncoderParameterBandwidth     = 0x0203
	aacEncoderParameterTransmux      = 0x0300
	aacEncoderParameterSignalingMode = 0x0302
	aacObjectTypeLC                  = 2
	aacBitrateModeCBR                = 0
	aacAfterburnerOn                 = 1
	aacTransmuxRaw                   = 0
	aacSignalingImplicit             = 0
	aacBufferInAudioData             = 0
	aacBufferOutBitstreamData        = 3
	aacEncoderOK                     = 0
	aacEncoderEOF                    = 0x80
	aacAllModules                    = 0
	aacMaximumChannels               = 2
	aacConfigBufferSize              = 64
	aacVersionStringSize             = 32
	aacLibraryInfoEntries            = 64
	aacModuleEncoder                 = 4
	aacOutputBufferBytes             = 20480
	aacFlushSamples                  = -1
	aacEncoderInvalidConfig          = 0x23
	aacMinimumBitrate                = 8000
	aacMaximumBitratePerChannel      = 264600
)

type aacBufferDescriptor struct {
	numberOfBuffers   int32
	buffers           *unsafe.Pointer
	bufferIdentifiers *int32
	bufferSizes       *int32
	elementSizes      *int32
}

type aacInputArguments struct {
	numberOfInputSamples int32
	numberOfAncBytes     int32
}

type aacOutputArguments struct {
	numberOfOutputBytes  int32
	numberOfInputSamples int32
	numberOfAncBytes     int32
	bitReservoirState    int32
}

type aacInfo struct {
	maximumOutputBytes uint32
	maximumAncBytes    uint32
	inputFillLevel     uint32
	inputChannels      uint32
	frameLength        uint32
	delay              uint32
	coreDelay          uint32
	configBuffer       [aacConfigBufferSize]byte
	configSize         uint32
}

type aacLibraryInfo struct {
	title         *byte
	buildDate     *byte
	buildTime     *byte
	moduleID      int32
	version       int32
	flags         uint32
	versionString [aacVersionStringSize]byte
}

type FDKLibrary struct {
	open       *windows.LazyProc
	setParam   *windows.LazyProc
	encode     *windows.LazyProc
	info       *windows.LazyProc
	closeProc  *windows.LazyProc
	getLibInfo *windows.LazyProc
}

func LoadFDKLibrary(path string) (FDKLibrary, error) {
	if !isFile(path) {
		return FDKLibrary{}, fmt.Errorf("%w: %s not found", ErrFDKUnavailable, path)
	}
	dll := windows.NewLazyDLL(path)
	if err := dll.Load(); err != nil {
		return FDKLibrary{}, fmt.Errorf("%w: %w", ErrFDKUnavailable, err)
	}
	library := FDKLibrary{
		open: dll.NewProc("aacEncOpen"), setParam: dll.NewProc("aacEncoder_SetParam"), encode: dll.NewProc("aacEncEncode"),
		info: dll.NewProc("aacEncInfo"), closeProc: dll.NewProc("aacEncClose"), getLibInfo: dll.NewProc("aacEncGetLibInfo"),
	}
	if err := library.findProcedures(); err != nil {
		return FDKLibrary{}, fmt.Errorf("%w: %w", ErrFDKUnavailable, err)
	}
	return library, nil
}

func (library FDKLibrary) findProcedures() error {
	for _, procedure := range []*windows.LazyProc{library.open, library.setParam, library.encode, library.info, library.closeProc, library.getLibInfo} {
		if err := procedure.Find(); err != nil {
			return err
		}
	}
	return nil
}

func (library FDKLibrary) Version() (string, error) {
	entries := make([]aacLibraryInfo, aacLibraryInfoEntries)
	code, _, _ := library.getLibInfo.Call(uintptr(unsafe.Pointer(&entries[0])))
	runtime.KeepAlive(entries)
	if code != aacEncoderOK {
		return "", FDKError{Code: int(code), Operation: "aacEncGetLibInfo"}
	}
	for _, entry := range entries {
		if entry.moduleID == aacModuleEncoder {
			return windows.ByteSliceToString(entry.versionString[:]), nil
		}
	}
	return "", FDKError{Code: int(code), Operation: "aacEncGetLibInfo"}
}

type FDKSession struct {
	library   FDKLibrary
	handle    uintptr
	channels  int
	info      aacInfo
	output    []byte
	isFlushed bool
}

func OpenFDKSession(library FDKLibrary, settings FDKSettings) (*FDKSession, error) {
	session := &FDKSession{library: library, channels: settings.Channels, output: make([]byte, aacOutputBufferBytes)}
	code, _, _ := library.open.Call(uintptr(unsafe.Pointer(&session.handle)), aacAllModules, aacMaximumChannels)
	if code != aacEncoderOK {
		return nil, FDKError{Code: int(code), Operation: "aacEncOpen"}
	}
	if err := session.configure(settings); err != nil {
		session.Close()
		return nil, err
	}
	return session, nil
}

func (session *FDKSession) configure(settings FDKSettings) error {
	if !isSupportedFDKSetting(settings) {
		return FDKError{Code: aacEncoderInvalidConfig, Operation: fmt.Sprintf("aacEncoder_SetParam(0x%04x)", aacEncoderParameterBitrate)}
	}
	for _, parameter := range fdkParameters(settings) {
		code, _, _ := session.library.setParam.Call(session.handle, parameter.id, parameter.value)
		if code != aacEncoderOK {
			return FDKError{Code: int(code), Operation: fmt.Sprintf("aacEncoder_SetParam(0x%04x)", parameter.id)}
		}
	}
	if code, _, _ := session.library.encode.Call(session.handle, 0, 0, 0, 0); code != aacEncoderOK {
		return FDKError{Code: int(code), Operation: "aacEncEncode(init)"}
	}
	code, _, _ := session.library.info.Call(session.handle, uintptr(unsafe.Pointer(&session.info)))
	if code != aacEncoderOK {
		return FDKError{Code: int(code), Operation: "aacEncInfo"}
	}
	return nil
}

func isSupportedFDKSetting(settings FDKSettings) bool {
	hasSupportedChannels := settings.Channels >= 1 && settings.Channels <= aacMaximumChannels
	return hasSupportedChannels && settings.Bitrate >= aacMinimumBitrate && settings.Bitrate <= aacMaximumBitratePerChannel*settings.Channels
}

type fdkParameter struct {
	id    uintptr
	value uintptr
}

func fdkParameters(settings FDKSettings) []fdkParameter {
	return []fdkParameter{
		{aacEncoderParameterAOT, aacObjectTypeLC},
		{aacEncoderParameterBitrateMode, aacBitrateModeCBR},
		{aacEncoderParameterSampleRate, uintptr(settings.SampleRate)},
		{aacEncoderParameterChannelMode, uintptr(settings.Channels)},
		{aacEncoderParameterBitrate, uintptr(settings.Bitrate)},
		{aacEncoderParameterBandwidth, uintptr(settings.Bandwidth)},
		{aacEncoderParameterAfterburner, aacAfterburnerOn},
		{aacEncoderParameterTransmux, aacTransmuxRaw},
		{aacEncoderParameterSignalingMode, aacSignalingImplicit},
	}
}

func (session *FDKSession) Encode(pcm []int16) ([][]byte, error) {
	frames := [][]byte{}
	for len(pcm) > 0 {
		frame, consumed, err := session.encodeCall(pcm, clampInt32(len(pcm)))
		if err != nil {
			return nil, err
		}
		if frame != nil {
			frames = append(frames, frame)
		}
		if consumed == 0 && frame == nil {
			break
		}
		pcm = pcm[consumed:]
	}
	return frames, nil
}

func (session *FDKSession) Flush() ([][]byte, error) {
	frames := [][]byte{}
	for !session.isFlushed {
		frame, _, err := session.encodeCall(nil, aacFlushSamples)
		if err != nil {
			return nil, err
		}
		if frame != nil {
			frames = append(frames, frame)
		}
	}
	return frames, nil
}

func (session *FDKSession) encodeCall(pcm []int16, sampleCount int32) ([]byte, int, error) {
	call := newEncodeCall(pcm, session.output)
	call.inputArguments.numberOfInputSamples = sampleCount
	code := call.run(session)
	if code == aacEncoderEOF {
		session.isFlushed = true
		return nil, 0, nil
	}
	if code != aacEncoderOK {
		return nil, 0, FDKError{Code: int(code), Operation: "aacEncEncode"}
	}
	return call.frame(), int(call.outputArguments.numberOfInputSamples), nil
}

func (session *FDKSession) AudioSpecificConfig() []byte {
	return append([]byte{}, session.info.configBuffer[:session.info.configSize]...)
}

func (session *FDKSession) EncoderDelay() int {
	return int(session.info.delay)
}

func (session *FDKSession) FrameLength() int {
	return int(session.info.frameLength)
}

func (session *FDKSession) Close() {
	if session.handle == 0 {
		return
	}
	_, _, _ = session.library.closeProc.Call(uintptr(unsafe.Pointer(&session.handle)))
	session.handle = 0
}

type encodeCall struct {
	inputBuffer     unsafe.Pointer
	outputBuffer    unsafe.Pointer
	inputIdentifier int32
	outputIdentity  int32
	inputSize       int32
	outputSize      int32
	elementSize     int32
	outputElement   int32
	input           aacBufferDescriptor
	output          aacBufferDescriptor
	inputArguments  aacInputArguments
	outputArguments aacOutputArguments
	pcm             []int16
	bitstream       []byte
}

func newEncodeCall(pcm []int16, bitstream []byte) *encodeCall {
	call := &encodeCall{
		inputIdentifier: aacBufferInAudioData, outputIdentity: aacBufferOutBitstreamData,
		inputSize: clampInt32(len(pcm) * pcmSampleBytes), outputSize: clampInt32(len(bitstream)),
		elementSize: pcmSampleBytes, outputElement: 1, pcm: pcm, bitstream: bitstream,
	}
	if len(pcm) > 0 {
		call.inputBuffer = unsafe.Pointer(&pcm[0])
	}
	call.outputBuffer = unsafe.Pointer(&bitstream[0])
	call.input = aacBufferDescriptor{numberOfBuffers: 1, buffers: &call.inputBuffer, bufferIdentifiers: &call.inputIdentifier, bufferSizes: &call.inputSize, elementSizes: &call.elementSize}
	call.output = aacBufferDescriptor{numberOfBuffers: 1, buffers: &call.outputBuffer, bufferIdentifiers: &call.outputIdentity, bufferSizes: &call.outputSize, elementSizes: &call.outputElement}
	return call
}

func (call *encodeCall) run(session *FDKSession) uintptr {
	var pinner runtime.Pinner
	defer pinner.Unpin()
	pinner.Pin(call)
	pinner.Pin(&call.bitstream[0])
	if len(call.pcm) > 0 {
		pinner.Pin(&call.pcm[0])
	}
	code, _, _ := session.library.encode.Call(session.handle,
		uintptr(unsafe.Pointer(&call.input)), uintptr(unsafe.Pointer(&call.output)),
		uintptr(unsafe.Pointer(&call.inputArguments)), uintptr(unsafe.Pointer(&call.outputArguments)))
	runtime.KeepAlive(call)
	return code
}

func (call *encodeCall) frame() []byte {
	if call.outputArguments.numberOfOutputBytes <= 0 {
		return nil
	}
	return append([]byte{}, call.bitstream[:call.outputArguments.numberOfOutputBytes]...)
}
