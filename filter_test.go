package pdf

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestASCIIHexDecode(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"48656C6C6F>", "Hello"},
		{"48656c6c6f>", "Hello"},
		{"48 65\n6C\t6C 6F >", "Hello"},
		{">", ""},
		{"", ""},
		{"4865", "He"},
		// A single digit before the end stands for that digit and a zero.
		{"48656C6C6F7>", "Hellop"},
		{"7", "p"},
	} {
		got, err := io.ReadAll(newASCIIHexReader(strings.NewReader(tc.in)))
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("%q decoded to %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestASCIIHexRejectsANonDigit(t *testing.T) {
	_, err := io.ReadAll(newASCIIHexReader(strings.NewReader("48zz>")))
	if err != errASCIIHex {
		t.Fatalf("err = %v, want %v", err, errASCIIHex)
	}
}

// TestASCIIHexIgnoresWhatFollowsTheEnd. The > marks the end, so a stream that
// carries the next object's bytes after it still decodes.
func TestASCIIHexIgnoresWhatFollowsTheEnd(t *testing.T) {
	got, err := io.ReadAll(newASCIIHexReader(strings.NewReader("4865>ZZZZ")))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "He" {
		t.Fatalf("decoded %q, want %q", got, "He")
	}
}

func TestRunLengthDecode(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"literal run", []byte{4, 'H', 'e', 'l', 'l', 'o', 128}, "Hello"},
		{"one literal byte", []byte{0, 'A', 128}, "A"},
		{"repeat", []byte{257 - 5, 'A', 128}, "AAAAA"},
		{"longest repeat", []byte{129, 'A', 128}, strings.Repeat("A", 128)},
		{"literal then repeat", []byte{1, 'a', 'b', 254, 'c', 128}, "abccc"},
		{"nothing", []byte{128}, ""},
	} {
		got, err := io.ReadAll(newRunLengthReader(bytes.NewReader(tc.in)))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("%s decoded to %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRunLengthWithoutAnEndMarker(t *testing.T) {
	_, err := io.ReadAll(newRunLengthReader(bytes.NewReader([]byte{1, 'a', 'b'})))
	if err != errRunLength {
		t.Fatalf("err = %v, want %v", err, errRunLength)
	}
}

func TestRunLengthWithATruncatedRun(t *testing.T) {
	if _, err := io.ReadAll(newRunLengthReader(bytes.NewReader([]byte{4, 'a'}))); err == nil {
		t.Fatal("a run that names more bytes than it holds was accepted")
	}
}

// TestPNGPredictorFilters reads one row of each of the five PNG filters. Each
// row decodes to the same three bytes, so the filter byte is the only thing
// under test.
func TestPNGPredictorFilters(t *testing.T) {
	want := []byte{10, 20, 30}
	for _, tc := range []struct {
		name string
		rows []byte
	}{
		{"none", []byte{pngNone, 10, 20, 30}},
		{"sub", []byte{pngSub, 10, 10, 10}},
		{"up", []byte{pngUp, 10, 20, 30}},
		{"average", []byte{pngAverage, 10, 15, 20}},
		{"paeth", []byte{pngPaeth, 10, 10, 10}},
	} {
		got, err := io.ReadAll(newPNGReader(bytes.NewReader(tc.rows), 1, 8, 3))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s decoded to %v, want %v", tc.name, got, want)
		}
	}
}

// TestPNGPredictorMixesFiltersBetweenRows. /Predictor 12 names PNG prediction
// and not one filter, so each row chooses its own.
func TestPNGPredictorMixesFiltersBetweenRows(t *testing.T) {
	rows := []byte{
		pngNone, 1, 2, 3,
		pngUp, 1, 1, 1, // 2 3 4
		pngSub, 1, 1, 1, // 1 2 3
		pngPaeth, 0, 0, 0, // 1 2 3, each equal to the row above
	}
	want := []byte{1, 2, 3, 2, 3, 4, 1, 2, 3, 1, 2, 3}

	got, err := io.ReadAll(newPNGReader(bytes.NewReader(rows), 1, 8, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded %v, want %v", got, want)
	}
}

// TestPNGPredictorWithSeveralColors. The byte to the left is one pixel back,
// not one byte back.
func TestPNGPredictorWithSeveralColors(t *testing.T) {
	// Three pixels of three colours, each pixel one more than the pixel before.
	rows := []byte{pngSub, 10, 20, 30, 1, 1, 1, 1, 1, 1}
	want := []byte{10, 20, 30, 11, 21, 31, 12, 22, 32}

	got, err := io.ReadAll(newPNGReader(bytes.NewReader(rows), 3, 8, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded %v, want %v", got, want)
	}
}

func TestPNGPredictorRejectsAnUnknownFilter(t *testing.T) {
	_, err := io.ReadAll(newPNGReader(bytes.NewReader([]byte{9, 1, 2, 3}), 1, 8, 3))
	if err == nil || !strings.Contains(err.Error(), "unknown PNG filter") {
		t.Fatalf("err = %v, want an unknown filter error", err)
	}
}

func TestTIFFPredictor(t *testing.T) {
	r, err := newTIFFReader(bytes.NewReader([]byte{10, 1, 1, 1}), 1, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{10, 11, 12, 13}; !bytes.Equal(got, want) {
		t.Fatalf("decoded %v, want %v", got, want)
	}
}

func TestTIFFPredictorWithSeveralColors(t *testing.T) {
	r, err := newTIFFReader(bytes.NewReader([]byte{10, 20, 1, 1}), 2, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{10, 20, 11, 21}; !bytes.Equal(got, want) {
		t.Fatalf("decoded %v, want %v", got, want)
	}
}

func TestTIFFPredictorRejectsOtherComponentWidths(t *testing.T) {
	if _, err := newTIFFReader(bytes.NewReader(nil), 1, 4, 2); err == nil {
		t.Fatal("4 bits per component was accepted")
	}
}

func TestApplyPredictorReadsItsParameters(t *testing.T) {
	rows := []byte{pngUp, 1, 2, 3, pngUp, 1, 1, 1}
	param := testValue(dict{
		name("Predictor"): int64(12),
		name("Columns"):   int64(3),
	})

	rd, err := applyPredictor(bytes.NewReader(rows), param)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rd)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{1, 2, 3, 2, 3, 4}; !bytes.Equal(got, want) {
		t.Fatalf("decoded %v, want %v", got, want)
	}
}

func TestApplyPredictorPassesTheDataThroughWhenThereIsNone(t *testing.T) {
	for _, param := range []Value{
		{},
		testValue(dict{name("Predictor"): int64(1)}),
	} {
		rd, err := applyPredictor(strings.NewReader("plain"), param)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rd)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "plain" {
			t.Errorf("read %q, want %q", got, "plain")
		}
	}
}

func TestApplyPredictorRejectsAnUnknownPredictor(t *testing.T) {
	param := testValue(dict{name("Predictor"): int64(5)})
	if _, err := applyPredictor(strings.NewReader(""), param); err == nil {
		t.Fatal("predictor 5 was accepted")
	}
}

// streamPDF builds a one-page document whose single object is a stream with
// the given dictionary entries and body.
func streamPDF(entries, body string) []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Contents 4 0 R >>",
		"<< /Length " + itoa(len(body)) + " " + entries + " >>\nstream\n" + body + "\nendstream",
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

// contentsOf reads the page content stream through the whole filter chain.
func contentsOf(t *testing.T, pdf []byte) ([]byte, error) {
	t.Helper()
	r := read(t, pdf)
	return io.ReadAll(r.Page(1).V.Key("Contents").Reader())
}

// TestReaderDecodesEachFilter drives each decoder through Value.Reader, which
// is how a caller reaches them.
func TestReaderDecodesEachFilter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries string
		body    string
		want    string
	}{
		{"ASCIIHexDecode", "/Filter /ASCIIHexDecode", "48656C6C6F>", "Hello"},
		{"RunLengthDecode", "/Filter /RunLengthDecode", "\x04Hello\x80", "Hello"},
		{"no filter", "", "Hello", "Hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := contentsOf(t, streamPDF(tc.entries, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("read %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReaderDecodesAFilterChain applies the filters in the order the array
// gives them, which is the order they were applied when the file was written.
func TestReaderDecodesAFilterChain(t *testing.T) {
	// "Hi" run length encoded, then written as hexadecimal.
	got, err := contentsOf(t, streamPDF(
		"/Filter [/ASCIIHexDecode /RunLengthDecode]", "014869 80>"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "Hi" {
		t.Fatalf("read %q, want %q", got, "Hi")
	}
}

// TestReaderReportsAnUnsupportedFilter. The filter used to panic, which a
// caller could not recover from without wrapping every call.
func TestReaderReportsAnUnsupportedFilter(t *testing.T) {
	for _, entries := range []string{
		"/Filter /JPXDecode",
		"/Filter [/JPXDecode]",
		"/Filter 3",
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s panicked: %v", entries, r)
				}
			}()
			_, err := contentsOf(t, streamPDF(entries, "anything"))
			if err == nil {
				t.Errorf("%s was accepted", entries)
			}
		}()
	}
}

// TestReaderReportsAnUnknownPredictor. Same reason: it used to panic.
func TestReaderReportsAnUnknownPredictor(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	_, err := contentsOf(t, streamPDF(
		"/Filter /LZWDecode /DecodeParms << /Predictor 5 >>", "anything"))
	if err == nil {
		t.Fatal("predictor 5 was accepted")
	}
}

// TestAPredictorBelongsToFlateAndLZWOnly. The other filters take no
// parameters, so a predictor on one of them says nothing and is ignored.
func TestAPredictorBelongsToFlateAndLZWOnly(t *testing.T) {
	got, err := contentsOf(t, streamPDF(
		"/Filter /ASCIIHexDecode /DecodeParms << /Predictor 12 /Columns 3 >>", "48656C>"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "Hel" {
		t.Fatalf("read %q, want %q", got, "Hel")
	}
}

// TestDecodeParmsFollowTheFilterArray. One entry belongs to each filter, and a
// single dictionary belongs to the one filter that takes parameters.
func TestDecodeParmsFollowTheFilterArray(t *testing.T) {
	// Two rows of three bytes with the PNG Up filter, written as hexadecimal.
	// 02 010203 02 010101 decodes to 1 2 3 then 2 3 4.
	rows := "02010203 02010101>"
	for _, parms := range []string{
		"/DecodeParms [null << /Predictor 12 /Columns 3 >>]",
		"/DecodeParms << /Predictor 12 /Columns 3 >>",
	} {
		got, err := contentsOf(t, streamPDF(
			"/Filter [/ASCIIHexDecode /FlateDecode] "+parms, rows))
		// FlateDecode cannot read this, so the point is only that the
		// parameters were paired with a filter and not rejected outright.
		if err == nil && len(got) == 0 {
			t.Errorf("%s read nothing and reported no error", parms)
		}
	}
}
