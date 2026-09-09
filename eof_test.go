package pdf

import (
	"bytes"
	"strings"
	"testing"
)

// Finding the end of a document, which is not always the end of the file.
//
// The specification puts %%EOF on the last line. Real files do not always: of
// 8,011 vendor datasheets, 133 carry a whole web page after the marker because
// the download appended one, and the median was 48 trailing bytes with the
// worst at 489 KB.

// withTail is a valid one page document with extra bytes written after it.
func withTail(tail string) []byte {
	return append(onePagePDF(), tail...)
}

// onePagePDF is the smallest document these tests need.
func onePagePDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
	})
}

func mustRead(t *testing.T, data []byte) *Reader {
	t.Helper()
	r, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if got := r.NumPage(); got != 1 {
		t.Fatalf("NumPage = %d, want 1", got)
	}
	return r
}

func TestTrailingBytesAfterTheMarker(t *testing.T) {
	for _, tc := range []struct {
		name string
		tail string
	}{
		{"nothing", ""},
		{"whitespace", "\n\n   \r\n"},
		{"a short line", "\nappended by the download\n"},
		{"a web page", "\n<html><body>" + strings.Repeat("<p>page not found</p>", 40) + "</body></html>"},
		{"past one chunk", "\n" + strings.Repeat("x", eofChunk*3)},
		{"past every chunk", "\n" + strings.Repeat("x", 500_000)},
	} {
		t.Run(tc.name, func(t *testing.T) { mustRead(t, withTail(tc.tail)) })
	}
}

// TestAMarkerStraddlingTwoChunks. The scan reads the file backwards a chunk at
// a time, so a marker that falls across the join must still be seen. The tail
// is sized to put one byte of the marker in the older chunk.
func TestAMarkerStraddlingTwoChunks(t *testing.T) {
	base := onePagePDF()
	for cut := 1; cut < len("%%EOF"); cut++ {
		// Place the marker so that eofChunk bytes from the end lands inside it.
		pad := eofChunk - cut
		data := append(append([]byte{}, base...), strings.Repeat("x", pad)...)
		if bytes.LastIndex(data, []byte("%%EOF")) < 0 {
			t.Fatal("the fixture lost its marker")
		}
		mustRead(t, data)
	}
}

// TestTheLastMarkerIsTheOneFound. A document that has been updated in place
// carries an earlier %%EOF as well, and the later one is the current end.
func TestTheLastMarkerIsTheOneFound(t *testing.T) {
	data := []byte("%PDF-1.4\nfirst body\nstartxref\n9\n%%EOF\nsecond body\nstartxref\n9\n%%EOF\n")
	want := int64(bytes.LastIndex(data, []byte("%%EOF")))

	got, ok := findEOF(bytes.NewReader(data), int64(len(data)))
	if !ok {
		t.Fatal("no marker found")
	}
	if got != want {
		t.Errorf("marker at %d, want the last one at %d", got, want)
	}
}

// TestTheStartxrefBelongsToItsMarker. The line is read from in front of the
// marker, so the one that belongs to an earlier marker is not picked up.
func TestTheStartxrefBelongsToItsMarker(t *testing.T) {
	data := []byte("%PDF-1.4\nbody\nstartxref\n11\n%%EOF\nmore\nstartxref\n22\n%%EOF\n")
	pos, err := findStartxref(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	want := int64(bytes.LastIndex(data, []byte("startxref")))
	if pos != want {
		t.Errorf("startxref at %d, want the last one at %d", pos, want)
	}
}

// TestAMarkerInTrailingContentIsSkipped. Appended text that holds the marker
// has no startxref line in front of it, so the real end of the document is
// found behind it.
//
// The appended text is longer than the window that is read in front of a
// marker. A shorter tail would let the document's own startxref line fall
// inside that window, and the first candidate would be accepted for the wrong
// reason.
func TestAMarkerInTrailingContentIsSkipped(t *testing.T) {
	tail := "\n" + strings.Repeat("appended by the download. ", startxrefWindow/26+40) +
		"\nthe report ends with %%EOF and says so\n"
	mustRead(t, withTail(tail))
}

func TestAFileWithNoMarkerIsRejected(t *testing.T) {
	data := []byte("%PDF-1.4\n" + strings.Repeat("x", eofChunk*2))
	_, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err == nil || !strings.Contains(err.Error(), "%%EOF") {
		t.Fatalf("err = %v, want a missing %%%%EOF error", err)
	}
}

// TestAMarkerWithNoStartxrefIsRejected, and says which of the two is missing.
func TestAMarkerWithNoStartxrefIsRejected(t *testing.T) {
	data := []byte("%PDF-1.4\n" + strings.Repeat("x", 200) + "\n%%EOF\n")
	_, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err == nil || !strings.Contains(err.Error(), "startxref") {
		t.Fatalf("err = %v, want a missing startxref error", err)
	}
}

// TestAFileShorterThanTheLookback. The old lookback read a fixed 100 bytes
// from the end and asked the file for a negative offset when it was smaller.
func TestAFileShorterThanTheLookback(t *testing.T) {
	if len(onePagePDF()) >= eofChunk {
		t.Skip("the fixture is no longer smaller than one chunk")
	}
	mustRead(t, onePagePDF())
}

// TestManyMarkersGiveUp. A file that is nothing but markers must not be walked
// to its start one marker at a time.
func TestManyMarkersGiveUp(t *testing.T) {
	data := []byte("%PDF-1.4\n" + strings.Repeat("%%EOF\n", eofCandidates*4))
	_, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err == nil {
		t.Fatal("a file of markers was accepted")
	}
}
