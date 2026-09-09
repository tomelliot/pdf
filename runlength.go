package pdf

import (
	"errors"
	"io"
)

// RunLengthDecode.
//
// The data is a series of runs. A length byte below 128 is followed by that
// many bytes plus one, which are copied out. A length byte above 128 is
// followed by one byte, which is repeated 257 minus the length byte times.
// The length byte 128 ends the data.
const runLengthEOD = 128

var errRunLength = errors.New("malformed run length data: no end of data marker")

type runLengthReader struct {
	r    io.Reader
	buf  [128]byte
	pend []byte
	err  error
}

func newRunLengthReader(r io.Reader) *runLengthReader {
	return &runLengthReader{r: r}
}

func (d *runLengthReader) Read(b []byte) (int, error) {
	for len(d.pend) == 0 {
		if d.err != nil {
			return 0, d.err
		}
		d.step()
	}
	n := copy(b, d.pend)
	d.pend = d.pend[n:]
	return n, nil
}

func (d *runLengthReader) step() {
	var length [1]byte
	if _, err := io.ReadFull(d.r, length[:]); err != nil {
		if err == io.EOF {
			// A writer that leaves the marker out still gave us every byte.
			err = errRunLength
		}
		d.err = err
		return
	}

	switch n := int(length[0]); {
	case n == runLengthEOD:
		d.err = io.EOF
	case n < runLengthEOD:
		if _, err := io.ReadFull(d.r, d.buf[:n+1]); err != nil {
			d.err = err
			return
		}
		d.pend = d.buf[:n+1]
	default:
		var value [1]byte
		if _, err := io.ReadFull(d.r, value[:]); err != nil {
			d.err = err
			return
		}
		count := 257 - n
		for i := 0; i < count; i++ {
			d.buf[i] = value[0]
		}
		d.pend = d.buf[:count]
	}
}
