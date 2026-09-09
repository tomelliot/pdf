package pdf

import (
	"bytes"
	"compress/lzw"
	"io"
	"strings"
	"testing"
)

func decodeLZW(t *testing.T, data []byte, param Value) ([]byte, error) {
	t.Helper()
	return io.ReadAll(newLZWReader(bytes.NewReader(data), param))
}

// TestLZWSpecExample decodes the example in the PDF specification, table 7.8.
// The nine encoded bytes stand for ten bytes, and reading them uses every part
// of the decoder: the clear code, a code the encoder used in the step that
// added it, an entry of more than one byte, and the end of data code.
func TestLZWSpecExample(t *testing.T) {
	encoded := []byte{0x80, 0x0B, 0x60, 0x50, 0x22, 0x0C, 0x0C, 0x85, 0x01}
	want := []byte{45, 45, 45, 45, 45, 65, 45, 45, 45, 66}

	got, err := decodeLZW(t, encoded, Value{})
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded = %v, want %v", got, want)
	}
}

// TestLZWReadsWhatAnEncoderWrote decodes streams from compress/lzw, which
// writes the same packing and grows the code width one code late. The
// parameter /EarlyChange 0 says so.
func TestLZWReadsWhatAnEncoderWrote(t *testing.T) {
	lateChange := testValue(dict{name("EarlyChange"): int64(0)})
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"one byte", []byte("A")},
		{"repeat", bytes.Repeat([]byte("A"), 300)},
		{"two byte cycle", bytes.Repeat([]byte("AB"), 500)},
		{"text", []byte(strings.Repeat("the quick brown fox ", 200))},
		{"every byte", allBytes()},
		{"past the first width change", bytes.Repeat(allBytes(), 3)},
		{"past every width change", bytes.Repeat(allBytes(), 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeLZW(t, encodeLZW(t, tc.data), lateChange)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if !bytes.Equal(got, tc.data) {
				t.Fatalf("decoded %d bytes, want %d", len(got), len(tc.data))
			}
		})
	}
}

// TestLZWEarlyChangeIsRead. The two settings put the width change one code
// apart, so a stream written for one does not read as the other.
func TestLZWEarlyChangeIsRead(t *testing.T) {
	data := bytes.Repeat(allBytes(), 3)
	encoded := encodeLZW(t, data)

	late, err := decodeLZW(t, encoded, testValue(dict{name("EarlyChange"): int64(0)}))
	if err != nil {
		t.Fatal(err)
	}
	early, _ := decodeLZW(t, encoded, Value{})
	if bytes.Equal(early, late) {
		t.Fatal("both settings decoded the same, so EarlyChange was not read")
	}
}

func TestLZWRejectsACodeItCannotHold(t *testing.T) {
	// A first code of 300 names a table entry that does not exist yet.
	var w lzwWriter
	w.write(300, 9)
	w.flush()

	if _, err := decodeLZW(t, w.out, Value{}); err != errLZWCode {
		t.Fatalf("err = %v, want %v", err, errLZWCode)
	}
}

func TestLZWStopsAtTheEndOfData(t *testing.T) {
	var w lzwWriter
	w.write(lzwClear, 9)
	w.write('A', 9)
	w.write(lzwEOD, 9)
	w.write('B', 9) // after the end marker, and must not be read
	w.flush()

	got, err := decodeLZW(t, w.out, Value{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "A" {
		t.Fatalf("decoded %q, want %q", got, "A")
	}
}

// TestLZWClearResetsTheTable. An encoder sends the clear code when the table
// fills, and may send it at any other time.
func TestLZWClearResetsTheTable(t *testing.T) {
	var w lzwWriter
	w.write(lzwClear, 9)
	w.write('A', 9)
	w.write('B', 9)
	w.write(lzwClear, 9) // 258 is free again after this
	w.write('C', 9)
	w.write('D', 9)
	w.write(258, 9) // C followed by D
	w.write(lzwEOD, 9)
	w.flush()

	got, err := decodeLZW(t, w.out, Value{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ABCDCD" {
		t.Fatalf("decoded %q, want %q", got, "ABCDCD")
	}
}

func TestLZWTruncatedDataEndsWithoutHanging(t *testing.T) {
	var w lzwWriter
	w.write(lzwClear, 9)
	w.write('A', 9)
	w.flush() // no end of data code

	got, err := decodeLZW(t, w.out, Value{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "A" {
		t.Fatalf("decoded %q, want %q", got, "A")
	}
}

func allBytes() []byte {
	out := make([]byte, 256)
	for i := range out {
		out[i] = byte(i)
	}
	return out
}

// encodeLZW writes data the way compress/lzw does, which is the same packing
// with the width change one code late.
func encodeLZW(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := lzw.NewWriter(&buf, lzw.MSB, 8)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// lzwWriter packs codes with the most significant bit first. It lets a test
// state the exact codes a decoder must read.
type lzwWriter struct {
	out   []byte
	bits  uint32
	nbits uint
}

func (w *lzwWriter) write(code int, width uint) {
	w.bits = w.bits<<width | uint32(code)
	w.nbits += width
	for w.nbits >= 8 {
		w.nbits -= 8
		w.out = append(w.out, byte(w.bits>>w.nbits))
	}
}

func (w *lzwWriter) flush() {
	if w.nbits > 0 {
		w.out = append(w.out, byte(w.bits<<(8-w.nbits)))
		w.nbits = 0
	}
}
