package runtime

import (
	"io"
	"sync"
)

// Wraps an [io.Reader] and signals when it returns [io.EOF].
//
// The done channel is closed exactly once on the first EOF, making it safe to
// use from multiple goroutines.
type doneReader struct {
	r    io.Reader
	once sync.Once
	done chan struct{}
}

// Creates a new [doneReader] wrapping the given reader.
func newDoneReader(r io.Reader) *doneReader {
	return &doneReader{r: r, done: make(chan struct{})}
}

// Delegates to the underlying reader.
//
// Closes the done channel on the first [io.EOF]. Non-EOF errors are returned
// without closing the channel.
func (d *doneReader) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	if err == io.EOF {
		d.once.Do(func() { close(d.done) })
	}
	return n, err
}
