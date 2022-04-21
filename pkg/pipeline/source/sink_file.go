package source

import (
	"io"
	"os"
)

func createFileSink(filename string) (io.WriteCloser, error) {
	return os.Create(filename)
}
