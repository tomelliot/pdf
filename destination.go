package pdf

// An outline entry names a page with a /Dest, or with an /A action of subtype
// /GoTo that holds the destination in /D. Either one is an explicit array that
// starts with a reference to the page, or a name that the document defines
// elsewhere. PDF 1.1 holds named destinations in the catalog /Dests
// dictionary. PDF 1.2 and later hold them in a name tree at /Names /Dests.

// maxDepth limits the descent through the page tree, through a name tree, and
// through a chain of named destinations. A malformed document can make any of
// the three cyclic.
const maxDepth = 32

// destinations resolves a destination to a page number.
type destinations struct {
	pages map[objptr]int // page number of each page object, from 1
	tree  Value          // catalog /Names /Dests
	dict  Value          // catalog /Dests
}

func (r *Reader) newDestinations() *destinations {
	root := r.Trailer().Key("Root")
	return &destinations{
		pages: r.pageNumbers(),
		tree:  root.Key("Names").Key("Dests"),
		dict:  root.Key("Dests"),
	}
}

// pageNumbers walks the page tree and numbers the pages in reading order.
//
// A page that the document writes in place, and not as an indirect reference,
// holds the object pointer of the node above it. Such a page is counted but
// not indexed, because no destination can point at it.
func (r *Reader) pageNumbers() map[objptr]int {
	numbers := make(map[objptr]int)
	count := 0
	var walk func(node Value, depth int)
	walk = func(node Value, depth int) {
		if depth > maxDepth {
			return
		}
		kids := node.Key("Kids")
		for i := 0; i < kids.Len(); i++ {
			kid := kids.Index(i)
			switch kid.Key("Type").Name() {
			case "Pages":
				walk(kid, depth+1)
			case "Page":
				count++
				if kid.ptr != node.ptr {
					numbers[kid.ptr] = count
				}
			}
		}
	}
	walk(r.Trailer().Key("Root").Key("Pages"), 0)
	return numbers
}

// pageOf returns the page number that an outline entry names, or 0 if the
// entry names no page this document holds.
func (d *destinations) pageOf(entry Value) int {
	if page := d.resolve(entry.Key("Dest"), 0); page != 0 {
		return page
	}
	action := entry.Key("A")
	if action.Key("S").Name() != "GoTo" {
		return 0
	}
	return d.resolve(action.Key("D"), 0)
}

// resolve returns the page number for one destination.
func (d *destinations) resolve(dest Value, depth int) int {
	if depth > maxDepth {
		return 0
	}
	switch dest.Kind() {
	case Array:
		return d.explicit(dest)
	case Name:
		return d.named(dest.Name(), depth)
	case String:
		return d.named(dest.RawString(), depth)
	}
	return 0
}

// explicit returns the page number that a destination array names.
//
// An array that starts with an integer is a remote destination. It names a
// page in a different file, so this returns 0 for it.
func (d *destinations) explicit(dest Value) int {
	if dest.Len() == 0 {
		return 0
	}
	return d.pages[dest.Index(0).ptr]
}

// named looks a destination up by name, in the name tree first.
func (d *destinations) named(name string, depth int) int {
	if dest := searchNames(d.tree, name, 0); !dest.IsNull() {
		return d.resolve(destValue(dest), depth+1)
	}
	if dest := d.dict.Key(name); !dest.IsNull() {
		return d.resolve(destValue(dest), depth+1)
	}
	return 0
}

// destValue unwraps a named destination. A name holds the destination array,
// or a dictionary that holds the array in /D.
func destValue(dest Value) Value {
	if dest.Kind() == Dict {
		return dest.Key("D")
	}
	return dest
}

// searchNames returns the value a name tree gives for name, or a null Value.
func searchNames(node Value, name string, depth int) Value {
	if depth > maxDepth {
		return Value{}
	}
	names := node.Key("Names")
	for i := 0; i+1 < names.Len(); i += 2 {
		if names.Index(i).RawString() == name {
			return names.Index(i + 1)
		}
	}
	kids := node.Key("Kids")
	for i := 0; i < kids.Len(); i++ {
		kid := kids.Index(i)
		if !inLimits(kid, name) {
			continue
		}
		if found := searchNames(kid, name, depth+1); !found.IsNull() {
			return found
		}
	}
	return Value{}
}

// inLimits reports whether a name tree node can hold name. /Limits gives the
// least and the greatest name in the subtree. A node without /Limits can hold
// any name.
func inLimits(node Value, name string) bool {
	limits := node.Key("Limits")
	if limits.Len() != 2 {
		return true
	}
	return name >= limits.Index(0).RawString() && name <= limits.Index(1).RawString()
}
