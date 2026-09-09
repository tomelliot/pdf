package pdf

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

// outlinePDF builds a three-page document with an outline. Each entry in
// entries is one object body for an outline item, chained through /Next in the
// order given. The extra objects go in as they are, so a test can add a name
// tree or a destination dictionary.
//
// The object numbers are fixed, so a test can name them:
//
//	1     catalog
//	2     page tree
//	3-5   the three pages
//	6     the outline root
//	7...  the extra objects, then the outline items
func outlinePDF(catalogExtra string, entries []string, extra []string) []byte {
	firstItem := 7 + len(extra)
	bodies := []string{
		fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /Outlines 6 0 R %s >>", catalogExtra),
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
	}

	outline := "<< /Type /Outlines >>"
	if len(entries) > 0 {
		outline = fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Count %d >>",
			firstItem, len(entries))
	}
	bodies = append(bodies, outline)
	bodies = append(bodies, extra...)

	for i, entry := range entries {
		body := fmt.Sprintf("<< %s /Parent 6 0 R", entry)
		if i+1 < len(entries) {
			body += fmt.Sprintf(" /Next %d 0 R", firstItem+i+1)
		}
		bodies = append(bodies, body+" >>")
	}
	return buildPDF(bodies)
}

// buildPDF assembles objects numbered from 1 into a PDF file. The catalog is
// object 1.
func buildPDF(bodies []string) []byte {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(bodies)+1)
	for i, body := range bodies {
		offsets[i+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}

	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(bodies)+1)
	for i := 1; i <= len(bodies); i++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(bodies)+1, xref)
	return pdf.Bytes()
}

func read(t *testing.T, data []byte) *Reader {
	t.Helper()
	r, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// pagesOf returns the page number of each outline entry that has a title, in
// the order the outline gives them.
func pagesOf(o Outline) []int {
	var pages []int
	if o.Title != "" {
		pages = append(pages, o.Page)
	}
	for _, child := range o.Child {
		pages = append(pages, pagesOf(child)...)
	}
	return pages
}

func TestExplicitDestination(t *testing.T) {
	pdf := outlinePDF("", []string{
		"/Title (One) /Dest [3 0 R /Fit]",
		"/Title (Two) /Dest [4 0 R /XYZ 0 700 0]",
		"/Title (Three) /Dest [5 0 R /Fit]",
	}, nil)

	got := pagesOf(read(t, pdf).Outline())
	want := []int{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pages = %v, want %v", got, want)
	}
}

func TestGoToActionDestination(t *testing.T) {
	pdf := outlinePDF("", []string{
		"/Title (One) /A << /S /GoTo /D [4 0 R /Fit] >>",
	}, nil)

	if got := pagesOf(read(t, pdf).Outline()); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("pages = %v, want [2]", got)
	}
}

// TestActionOtherThanGoToIsNotFollowed. A /GoToR action names a page in
// another file, and a /URI action names no page at all.
func TestActionOtherThanGoToIsNotFollowed(t *testing.T) {
	for _, action := range []string{
		"<< /S /GoToR /F (other.pdf) /D [2 /Fit] >>",
		"<< /S /URI /URI (https://example.com) >>",
	} {
		pdf := outlinePDF("", []string{"/Title (One) /A " + action}, nil)
		if got := pagesOf(read(t, pdf).Outline()); !reflect.DeepEqual(got, []int{0}) {
			t.Errorf("%s gave pages %v, want [0]", action, got)
		}
	}
}

// TestDestTakesPrecedenceOverAction. An entry should not carry both, and the
// destination is the one the reader is told to use.
func TestDestTakesPrecedenceOverAction(t *testing.T) {
	pdf := outlinePDF("", []string{
		"/Title (One) /Dest [3 0 R /Fit] /A << /S /GoTo /D [5 0 R /Fit] >>",
	}, nil)

	if got := pagesOf(read(t, pdf).Outline()); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("pages = %v, want [1]", got)
	}
}

func TestNamedDestinationInNameTree(t *testing.T) {
	pdf := outlinePDF("/Names << /Dests 7 0 R >>", []string{
		"/Title (One) /Dest (chapter.1)",
		"/Title (Two) /A << /S /GoTo /D (chapter.2) >>",
	}, []string{
		"<< /Names [(chapter.1) [4 0 R /Fit] (chapter.2) [5 0 R /Fit]] >>",
	})

	got := pagesOf(read(t, pdf).Outline())
	want := []int{2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pages = %v, want %v", got, want)
	}
}

// TestNamedDestinationInNameTreeWithKids walks an intermediate node. The
// /Limits array on each leaf tells the search which subtree to enter.
func TestNamedDestinationInNameTreeWithKids(t *testing.T) {
	pdf := outlinePDF("/Names << /Dests 7 0 R >>", []string{
		"/Title (One) /Dest (alpha)",
		"/Title (Two) /Dest (zulu)",
	}, []string{
		"<< /Kids [8 0 R 9 0 R] >>",
		"<< /Limits [(alpha) (mike)] /Names [(alpha) [3 0 R /Fit]] >>",
		"<< /Limits [(november) (zulu)] /Names [(zulu) [5 0 R /Fit]] >>",
	})

	got := pagesOf(read(t, pdf).Outline())
	want := []int{1, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pages = %v, want %v", got, want)
	}
}

// TestNameTreeLimitsDoNotHideAName. A node without /Limits holds any name, so
// the search must enter it.
func TestNameTreeLimitsDoNotHideAName(t *testing.T) {
	pdf := outlinePDF("/Names << /Dests 7 0 R >>", []string{
		"/Title (One) /Dest (zulu)",
	}, []string{
		"<< /Kids [8 0 R] >>",
		"<< /Names [(zulu) [4 0 R /Fit]] >>",
	})

	if got := pagesOf(read(t, pdf).Outline()); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("pages = %v, want [2]", got)
	}
}

// TestNamedDestinationIsADictionary. A named destination can hold the array in
// /D instead of holding the array itself.
func TestNamedDestinationIsADictionary(t *testing.T) {
	pdf := outlinePDF("/Names << /Dests 7 0 R >>", []string{
		"/Title (One) /Dest (chapter.1)",
	}, []string{
		"<< /Names [(chapter.1) << /D [5 0 R /Fit] >>] >>",
	})

	if got := pagesOf(read(t, pdf).Outline()); !reflect.DeepEqual(got, []int{3}) {
		t.Errorf("pages = %v, want [3]", got)
	}
}

// TestNamedDestinationInCatalogDests reads the PDF 1.1 form, where the catalog
// holds a dictionary keyed by name and the destination is a name object.
func TestNamedDestinationInCatalogDests(t *testing.T) {
	pdf := outlinePDF("/Dests 7 0 R", []string{
		"/Title (One) /Dest /chapter1",
	}, []string{
		"<< /chapter1 [4 0 R /Fit] >>",
	})

	if got := pagesOf(read(t, pdf).Outline()); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("pages = %v, want [2]", got)
	}
}

// TestNameTreeIsSearchedBeforeTheCatalogDictionary. A document that carries
// both must use the name tree.
func TestNameTreeIsSearchedBeforeTheCatalogDictionary(t *testing.T) {
	pdf := outlinePDF("/Names << /Dests 7 0 R >> /Dests 8 0 R", []string{
		"/Title (One) /Dest (target)",
	}, []string{
		"<< /Names [(target) [3 0 R /Fit]] >>",
		"<< /target [5 0 R /Fit] >>",
	})

	if got := pagesOf(read(t, pdf).Outline()); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("pages = %v, want [1]", got)
	}
}

func TestUnresolvableDestinationGivesZero(t *testing.T) {
	for _, entry := range []string{
		"/Title (no destination)",
		"/Title (empty array) /Dest []",
		"/Title (unknown name) /Dest (missing)",
		"/Title (not a page) /Dest [2 0 R /Fit]",
		"/Title (remote form) /Dest [1 /Fit]",
	} {
		pdf := outlinePDF("", []string{entry}, nil)
		if got := pagesOf(read(t, pdf).Outline()); !reflect.DeepEqual(got, []int{0}) {
			t.Errorf("%s gave pages %v, want [0]", entry, got)
		}
	}
}

// TestNestedEntriesAreResolved. Children are resolved with the same index as
// their parent.
func TestNestedEntriesAreResolved(t *testing.T) {
	pdf := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Outlines /First 7 0 R /Count 2 >>",
		"<< /Title (Chapter) /Dest [3 0 R /Fit] /First 8 0 R /Parent 6 0 R >>",
		"<< /Title (Section) /Dest [5 0 R /Fit] /Parent 7 0 R >>",
	})

	root := read(t, pdf).Outline()
	want := Outline{
		Child: []Outline{{
			Title: "Chapter",
			Page:  1,
			Child: []Outline{{Title: "Section", Page: 3}},
		}},
	}
	if !reflect.DeepEqual(root, want) {
		t.Errorf("Outline() = %+v, want %+v", root, want)
	}
}

// TestPageNumbersAgreeWithPage is the property that makes the number useful.
// An entry that reports page n must name the page that Page(n) returns.
func TestPageNumbersAgreeWithPage(t *testing.T) {
	pdf := outlinePDF("", []string{
		"/Title (One) /Dest [5 0 R /Fit]",
		"/Title (Two) /Dest [3 0 R /Fit]",
	}, nil)

	r := read(t, pdf)
	for _, entry := range r.Outline().Child {
		if entry.Page == 0 {
			t.Fatalf("%q resolved to no page", entry.Title)
		}
		want := r.Page(entry.Page)
		if want.V.IsNull() {
			t.Fatalf("%q names page %d, which Page returns as null", entry.Title, entry.Page)
		}
	}
}

// TestPageNumbersFollowANestedPageTree. Pages are numbered in reading order
// across the whole tree, not within one node.
func TestPageNumbersFollowANestedPageTree(t *testing.T) {
	pdf := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 3 >>",
		"<< /Type /Pages /Parent 2 0 R /Kids [4 0 R 5 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Outlines /First 8 0 R /Count 3 >>",
		"<< /Title (First) /Dest [4 0 R /Fit] /Next 9 0 R /Parent 7 0 R >>",
		"<< /Title (Second) /Dest [5 0 R /Fit] /Next 10 0 R /Parent 7 0 R >>",
		"<< /Title (Third) /Dest [6 0 R /Fit] /Parent 7 0 R >>",
	})

	got := pagesOf(read(t, pdf).Outline())
	want := []int{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pages = %v, want %v", got, want)
	}
}

func TestDocumentWithoutAnOutline(t *testing.T) {
	pdf := outlinePDF("", nil, nil)
	if got := read(t, pdf).Outline(); !reflect.DeepEqual(got, Outline{}) {
		t.Errorf("Outline() = %+v, want the zero Outline", got)
	}
}

// TestCyclicNameTreeTerminates. A tree that points at itself must not make the
// search run forever.
func TestCyclicNameTreeTerminates(t *testing.T) {
	pdf := outlinePDF("/Names << /Dests 7 0 R >>", []string{
		"/Title (One) /Dest (missing)",
	}, []string{
		"<< /Kids [7 0 R] >>",
	})

	if got := pagesOf(read(t, pdf).Outline()); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("pages = %v, want [0]", got)
	}
}

// TestCyclicNamedDestinationTerminates. A name that resolves to itself must
// not make the lookup run forever.
func TestCyclicNamedDestinationTerminates(t *testing.T) {
	pdf := outlinePDF("/Names << /Dests 7 0 R >>", []string{
		"/Title (One) /Dest (loop)",
	}, []string{
		"<< /Names [(loop) (loop)] >>",
	})

	if got := pagesOf(read(t, pdf).Outline()); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("pages = %v, want [0]", got)
	}
}

// TestCyclicPageTreeTerminates. A page tree node that holds itself must not
// make the walk run forever.
func TestCyclicPageTreeTerminates(t *testing.T) {
	pdf := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R >>",
		"<< /Type /Pages /Kids [2 0 R 3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Outlines /First 5 0 R /Count 1 >>",
		"<< /Title (One) /Dest [3 0 R /Fit] /Parent 4 0 R >>",
	})

	if got := pagesOf(read(t, pdf).Outline()); len(got) != 1 {
		t.Errorf("pages = %v, want one entry", got)
	}
}
