//go:build !darwin && !linux && !(windows && go1.25)

package link

import "os"

func openPort(path string, baud int) (*os.File, error) { return nil, ErrUnsupported }
