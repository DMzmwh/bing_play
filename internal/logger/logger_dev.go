//go:build !prod

package logger

import (
	"io"
	"os"
)

func newWriter(file io.Writer) io.Writer {
	return io.MultiWriter(
		os.Stdout,
		file,
	)
}
