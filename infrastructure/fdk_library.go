package infrastructure

import (
	"errors"
	"fmt"
)

var ErrFDKUnavailable = errors.New("FDK-AAC library is unavailable")

type FDKSettings struct {
	SampleRate int
	Channels   int
	Bitrate    int
	Bandwidth  int
}

type FDKError struct {
	Code      int
	Operation string
}

func (fdkError FDKError) Error() string {
	return fmt.Sprintf("%s failed with FDK-AAC error 0x%02x", fdkError.Operation, fdkError.Code)
}
