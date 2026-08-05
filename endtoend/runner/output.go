package runner

import (
	"io"
	"sync"
)

type lockedWriter struct {
	lock   *sync.Mutex
	writer io.Writer
}

func (writer *lockedWriter) Write(payload []byte) (int, error) {
	writer.lock.Lock()
	defer writer.lock.Unlock()
	return writer.writer.Write(payload)
}
