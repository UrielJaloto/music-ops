package infrastructure

import (
	"errors"
	"fmt"
	"math"
)

const pcmSampleBytes = 2

var ErrFDKUnavailable = errors.New("FDK-AAC library is unavailable")

func clampInt32(value int) int32 {
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	if value < math.MinInt32 {
		return math.MinInt32
	}
	return int32(value)
}

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
