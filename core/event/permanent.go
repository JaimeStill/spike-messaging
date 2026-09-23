package event

import "errors"

// permanentError marks a handler failure that redelivery cannot fix.
type permanentError struct{ err error }

func (p *permanentError) Error() string { return "permanent: " + p.err.Error() }
func (p *permanentError) Unwrap() error { return p.err }

// Permanent marks err as a handler failure that must never be redelivered;
// the provider terminates the delivery instead. The mark survives further
// wrapping, and errors.Is and errors.As still reach err. Permanent(nil) is
// nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether any error in err's tree was marked by
// [Permanent].
func IsPermanent(err error) bool {
	var p *permanentError
	return errors.As(err, &p)
}
