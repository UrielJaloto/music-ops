package businesserr

import "errors"

type businessError struct {
	message string
}

func (err businessError) Error() string {
	return err.message
}

func IsBusinessError(err error) bool {
	var target businessError
	return errors.As(err, &target)
}

func NewBusinessError(message string) error {
	return businessError{message: message}
}
