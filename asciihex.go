package pdf

import (
	"errors"
	"io"
)

// ASCIIHexDecode.
//
// Each byte is written as two hexadecimal digits. White space is ignored. The
// character > ends the data. A single digit before the end stands for that
// digit followed by a zero.

var errASCIIHex = errors.New("malformed ASCII hex data")

type asciiHexReader struct {
	r    io.Reader
	in   [512]byte
	out  [256]byte // two digits make one byte, so half of in is enough
	pend []byte
	odd  int // the first digit of a pair, or -1 when there is none
	err  error
}

func newASCIIHexReader(r io.Reader) *asciiHexReader {
	return &asciiHexReader{r: r, odd: -1}
}

func (d *asciiHexReader) Read(b []byte) (int, error) {
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

func (d *asciiHexReader) step() {
	n, err := d.r.Read(d.in[:])
	out := d.out[:0]
	for _, c := range d.in[:n] {
		if c == '>' {
			out = d.flush(out)
			d.pend = out
			d.err = io.EOF
			return
		}
		if isHexSpace(c) {
			continue
		}
		digit, ok := hexDigit(c)
		if !ok {
			d.err = errASCIIHex
			d.pend = out
			return
		}
		if d.odd < 0 {
			d.odd = digit
			continue
		}
		out = append(out, byte(d.odd<<4|digit))
		d.odd = -1
	}
	d.pend = out
	if err != nil {
		if err == io.EOF {
			d.pend = d.flush(d.pend)
		}
		d.err = err
	}
}

// flush writes out a digit left over at the end of the data. The missing
// second digit reads as zero.
func (d *asciiHexReader) flush(out []byte) []byte {
	if d.odd < 0 {
		return out
	}
	out = append(out, byte(d.odd<<4))
	d.odd = -1
	return out
}

func isHexSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == 0
}

func hexDigit(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}
