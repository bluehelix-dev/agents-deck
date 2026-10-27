//go:build windows

package term

import "errors"

// ErrUnsupported: Windows terminals have no tty the deck could write to.
var ErrUnsupported = errors.New("terminal focus isn't available on Windows")

const available = false

func write(string, string) error { return ErrUnsupported }
