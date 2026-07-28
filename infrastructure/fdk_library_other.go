//go:build !windows

package infrastructure

type FDKLibrary struct{}

func LoadFDKLibrary(string) (FDKLibrary, error) {
	return FDKLibrary{}, ErrFDKUnavailable
}

func (FDKLibrary) Version() (string, error) {
	return "", ErrFDKUnavailable
}

type FDKSession struct{}

func OpenFDKSession(FDKLibrary, FDKSettings) (*FDKSession, error) {
	return nil, ErrFDKUnavailable
}

func (*FDKSession) Encode([]int16) ([][]byte, error) {
	return nil, ErrFDKUnavailable
}

func (*FDKSession) Flush() ([][]byte, error) {
	return nil, ErrFDKUnavailable
}

func (*FDKSession) AudioSpecificConfig() []byte {
	return []byte{}
}

func (*FDKSession) EncoderDelay() int {
	return 0
}

func (*FDKSession) FrameLength() int {
	return 0
}

func (*FDKSession) Close() {}
