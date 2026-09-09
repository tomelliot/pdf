package pdf

import (
	"bufio"
	"errors"
	"io"
)

// LZWDecode, in the form PDF uses.
//
// Codes are 9 to 12 bits wide and are packed with the most significant bit
// first. Code 256 clears the table and code 257 ends the data. The table holds
// the 256 single bytes, those two codes, and then one entry for each code the
// decoder reads.
//
// /EarlyChange says when the code width grows. The default of 1 grows it one
// code before the table needs the extra bit. This is not the same as the LZW
// that compress/lzw reads, which is why the decoder is here.

const (
	lzwClear    = 256
	lzwEOD      = 257
	lzwFirst    = 258
	lzwMaxWidth = 12
	lzwMaxCode  = 1 << lzwMaxWidth
)

var errLZWCode = errors.New("malformed LZW data: code out of range")

type lzwReader struct {
	r io.ByteReader
	// early is 1 when the width grows one code early, and 0 when it does not.
	early int

	bits  uint32 // unread bits, in the low end
	nbits uint
	err   error

	width  uint // current code width
	next   int  // next free table entry
	prev   int  // previous code, or -1 after a clear
	prefix [lzwMaxCode]uint16
	suffix [lzwMaxCode]byte

	out []byte // decoded bytes not yet read
	tmp []byte // one entry, expanded
}

func newLZWReader(r io.Reader, param Value) *lzwReader {
	early := 1
	if v := param.Key("EarlyChange"); v.Kind() == Integer {
		early = int(v.Int64())
	}
	if early != 0 {
		early = 1
	}
	z := &lzwReader{r: bufio.NewReader(r), early: early}
	z.clear()
	return z
}

func (z *lzwReader) clear() {
	z.width = 9
	z.next = lzwFirst
	z.prev = -1
}

func (z *lzwReader) Read(b []byte) (int, error) {
	for len(z.out) == 0 {
		if z.err != nil {
			return 0, z.err
		}
		z.step()
	}
	n := copy(b, z.out)
	z.out = z.out[n:]
	return n, nil
}

// step reads one code and appends what it stands for to z.out.
func (z *lzwReader) step() {
	code, ok := z.code()
	if !ok {
		return
	}
	switch code {
	case lzwClear:
		z.clear()
		return
	case lzwEOD:
		z.err = io.EOF
		return
	}

	if z.prev < 0 {
		if code >= lzwClear {
			z.err = errLZWCode
			return
		}
		z.out = append(z.out, byte(code))
		z.prev = code
		return
	}

	var entry []byte
	switch {
	case code < z.next:
		entry = z.expand(code)
	case code == z.next:
		// The encoder used an entry in the same step that it added it, so the
		// entry is the previous one followed by its own first byte.
		entry = z.expand(z.prev)
		entry = append(entry, entry[0])
	default:
		z.err = errLZWCode
		return
	}

	z.out = append(z.out, entry...)
	if z.next < lzwMaxCode {
		z.prefix[z.next] = uint16(z.prev)
		z.suffix[z.next] = entry[0]
		z.next++
	}
	z.prev = code
	if z.width < lzwMaxWidth && z.next+z.early >= 1<<z.width {
		z.width++
	}
}

// expand writes the bytes a code stands for into z.tmp and returns it.
func (z *lzwReader) expand(code int) []byte {
	z.tmp = z.tmp[:0]
	for i := 0; code >= lzwFirst && i < lzwMaxCode; i++ {
		z.tmp = append(z.tmp, z.suffix[code])
		code = int(z.prefix[code])
	}
	z.tmp = append(z.tmp, byte(code))
	for i, j := 0, len(z.tmp)-1; i < j; i, j = i+1, j-1 {
		z.tmp[i], z.tmp[j] = z.tmp[j], z.tmp[i]
	}
	return z.tmp
}

// code reads the next code. It reports false when the data ends.
func (z *lzwReader) code() (int, bool) {
	for z.nbits < z.width {
		c, err := z.r.ReadByte()
		if err != nil {
			// A writer that leaves out the end code still gave every byte, so
			// the bits left over are padding.
			z.err = err
			return 0, false
		}
		z.bits = z.bits<<8 | uint32(c)
		z.nbits += 8
	}
	z.nbits -= z.width
	return int(z.bits>>z.nbits) & (1<<z.width - 1), true
}
