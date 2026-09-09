package pdf

import (
	"bytes"
	"io"
	"sync"
	"testing"
)

// countingReaderAt counts the reads a Reader makes of the file.
type countingReaderAt struct {
	at    io.ReaderAt
	mu    sync.Mutex
	reads int
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.mu.Lock()
	c.reads++
	c.mu.Unlock()
	return c.at.ReadAt(p, off)
}

func (c *countingReaderAt) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

func countedReader(t *testing.T, data []byte) (*Reader, *countingReaderAt) {
	t.Helper()
	counter := &countingReaderAt{at: bytes.NewReader(data)}
	r, err := NewReader(counter, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return r, counter
}

// TestAnObjectIsReadFromTheFileOnce. Without this, every use of a value reads
// and parses the object again, and a glyph width is three such reads.
func TestAnObjectIsReadFromTheFileOnce(t *testing.T) {
	r, counter := countedReader(t, buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
	}))

	root := r.Trailer().Key("Root")
	if root.Key("Pages").Key("Count").Int64() != 1 {
		t.Fatal("the document did not read")
	}
	before := counter.count()

	for i := 0; i < 50; i++ {
		if root.Key("Pages").Key("Count").Int64() != 1 {
			t.Fatal("a later read gave a different answer")
		}
	}
	if after := counter.count(); after != before {
		t.Errorf("%d more read(s) of the file for objects already read", after-before)
	}
}

// TestTheCacheGivesTheSameValue. A cached object must be the object, not a
// stale or partly built copy of it.
func TestTheCacheGivesTheSameValue(t *testing.T) {
	r, _ := countedReader(t, buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Dests 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /name [3 0 R /Fit] /number 42 /text (hello) >>",
	}))

	dests := r.Trailer().Key("Root").Key("Dests")
	for i := 0; i < 3; i++ {
		if got := dests.Key("number").Int64(); got != 42 {
			t.Fatalf("number = %d, want 42", got)
		}
		if got := dests.Key("text").RawString(); got != "hello" {
			t.Fatalf("text = %q, want %q", got, "hello")
		}
		if got := dests.Key("name").Len(); got != 2 {
			t.Fatalf("name has %d elements, want 2", got)
		}
	}
}

// TestACachedStreamStillReads. A stream value holds an offset into the file
// rather than the bytes, so reading it twice has to give the same bytes.
func TestACachedStreamStillReads(t *testing.T) {
	r, _ := countedReader(t, streamPDF("", "Hello"))

	contents := r.Page(1).V.Key("Contents")
	for i := 0; i < 3; i++ {
		got, err := io.ReadAll(contents.Reader())
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "Hello" {
			t.Fatalf("read %q, want %q", got, "Hello")
		}
	}
}

// TestTheCacheIsSafeToShare. A Reader is read only once it is open, so several
// goroutines may use one.
func TestTheCacheIsSafeToShare(t *testing.T) {
	r, _ := countedReader(t, buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
	}))

	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for n := 0; n < 40; n++ {
				if r.Page(1 + n%2).V.IsNull() {
					t.Error("a page read as null")
					return
				}
			}
		}()
	}
	wait.Wait()
}
