package pdf

import (
	"fmt"
	"io"
)

// A predictor makes a stream easier to compress. The encoder replaces each
// byte with its difference from a neighbour, and this reads the bytes back.
//
// /Predictor 2 is the TIFF predictor. Each component is the difference from
// the component to its left. /Predictor 10 and above is PNG prediction. Each
// row starts with a byte that names one of five filters, so the value of
// /Predictor beyond 10 says nothing: the row itself says which filter it used.

// The PNG row filters, from the PNG specification.
const (
	pngNone = iota
	pngSub
	pngUp
	pngAverage
	pngPaeth
)

// pngReader reads a PNG predicted stream.
type pngReader struct {
	r io.Reader
	// bpp is the number of bytes one pixel occupies, and the distance to the
	// component on the left. It is 1 when a component is smaller than a byte,
	// because the filters then work on whole bytes.
	bpp int
	// row holds the previous row, and then the current one. It has one byte
	// of slack at the front so that the byte to the left of the first pixel
	// reads as zero.
	row  []byte
	prev []byte
	tmp  []byte
	pend []byte
}

func newPNGReader(r io.Reader, colors, bits, columns int) *pngReader {
	width := rowBytes(colors, bits, columns)
	return &pngReader{
		r:    r,
		bpp:  pixelBytes(colors, bits),
		row:  make([]byte, width),
		prev: make([]byte, width),
		tmp:  make([]byte, width+1),
	}
}

func (p *pngReader) Read(b []byte) (int, error) {
	n := 0
	for len(b) > 0 {
		if len(p.pend) > 0 {
			m := copy(b, p.pend)
			n += m
			b = b[m:]
			p.pend = p.pend[m:]
			continue
		}
		if _, err := io.ReadFull(p.r, p.tmp); err != nil {
			return n, err
		}
		if err := p.unfilter(p.tmp[0], p.tmp[1:]); err != nil {
			return n, err
		}
		p.prev, p.row = p.row, p.prev
		p.pend = p.prev
	}
	return n, nil
}

// unfilter reverses one row filter, writing the result into p.row.
func (p *pngReader) unfilter(filter byte, data []byte) error {
	for i, cur := range data {
		var left, up, upLeft byte
		if i >= p.bpp {
			left = p.row[i-p.bpp]
			upLeft = p.prev[i-p.bpp]
		}
		up = p.prev[i]

		switch filter {
		case pngNone:
			p.row[i] = cur
		case pngSub:
			p.row[i] = cur + left
		case pngUp:
			p.row[i] = cur + up
		case pngAverage:
			p.row[i] = cur + byte((int(left)+int(up))/2)
		case pngPaeth:
			p.row[i] = cur + paeth(left, up, upLeft)
		default:
			return fmt.Errorf("unknown PNG filter %d", filter)
		}
	}
	return nil
}

// paeth picks the neighbour that the Paeth predictor says is closest.
func paeth(left, up, upLeft byte) byte {
	p := int(left) + int(up) - int(upLeft)
	pa, pb, pc := abs(p-int(left)), abs(p-int(up)), abs(p-int(upLeft))
	switch {
	case pa <= pb && pa <= pc:
		return left
	case pb <= pc:
		return up
	}
	return upLeft
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// tiffReader reads a stream predicted with the TIFF predictor. Only 8 bits
// per component is supported, which is the only width PDF writers use here.
type tiffReader struct {
	r      io.Reader
	colors int
	row    []byte
	pend   []byte
}

func newTIFFReader(r io.Reader, colors, bits, columns int) (io.Reader, error) {
	if bits != 8 {
		return nil, fmt.Errorf("TIFF predictor with %d bits per component is not supported", bits)
	}
	return &tiffReader{r: r, colors: colors, row: make([]byte, rowBytes(colors, bits, columns))}, nil
}

func (t *tiffReader) Read(b []byte) (int, error) {
	n := 0
	for len(b) > 0 {
		if len(t.pend) > 0 {
			m := copy(b, t.pend)
			n += m
			b = b[m:]
			t.pend = t.pend[m:]
			continue
		}
		if _, err := io.ReadFull(t.r, t.row); err != nil {
			return n, err
		}
		for i := t.colors; i < len(t.row); i++ {
			t.row[i] += t.row[i-t.colors]
		}
		t.pend = t.row
	}
	return n, nil
}

// rowBytes is the number of bytes one row occupies, rounded up.
func rowBytes(colors, bits, columns int) int {
	return (colors*bits*columns + 7) / 8
}

// pixelBytes is the number of bytes one pixel occupies, and never less than 1.
func pixelBytes(colors, bits int) int {
	if n := colors * bits / 8; n > 1 {
		return n
	}
	return 1
}

// predictorParams reads the decode parameters that a predictor needs. The
// defaults are the ones the PDF specification gives.
func predictorParams(param Value) (colors, bits, columns int) {
	colors, bits, columns = 1, 8, 1
	if v := param.Key("Colors"); v.Kind() == Integer {
		colors = int(v.Int64())
	}
	if v := param.Key("BitsPerComponent"); v.Kind() == Integer {
		bits = int(v.Int64())
	}
	if v := param.Key("Columns"); v.Kind() == Integer {
		columns = int(v.Int64())
	}
	return colors, bits, columns
}

// applyPredictor wraps rd so that it reads the predicted bytes back.
func applyPredictor(rd io.Reader, param Value) (io.Reader, error) {
	pred := param.Key("Predictor")
	if pred.Kind() == Null || pred.Int64() <= 1 {
		return rd, nil
	}
	colors, bits, columns := predictorParams(param)
	if colors < 1 || bits < 1 || columns < 1 {
		return nil, fmt.Errorf("invalid predictor parameters: %d colors, %d bits, %d columns",
			colors, bits, columns)
	}
	switch value := pred.Int64(); {
	case value == 2:
		return newTIFFReader(rd, colors, bits, columns)
	case value >= 10:
		return newPNGReader(rd, colors, bits, columns), nil
	default:
		return nil, fmt.Errorf("unknown predictor %d", value)
	}
}
