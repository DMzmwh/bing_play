//go:build prod

package logger

import "io"

func newWriter(file io.Writer) io.Writer {

	return file

}
