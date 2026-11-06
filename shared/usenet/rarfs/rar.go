package rarfs

import (
	"errors"
	"io"
)

type Volume interface {
	io.ReaderAt
	Size() (int64, error)
}

type frag struct {
	vol    int
	off    int64
	length int64
}

type Entry struct {
	Name       string
	Size       int64
	Streamable bool
	frags      []frag
}

type Archive struct {
	Entries []Entry
}

var (
	rar4Sig = []byte("Rar!\x1a\x07\x00")
	rar5Sig = []byte("Rar!\x1a\x07\x01\x00")
)

func Open(vols []Volume) (*Archive, error) {
	if len(vols) == 0 {
		return nil, errors.New("rarfs: no volumes")
	}
	head, err := readSlice(vols[0], 0, 8)
	if err != nil || len(head) < 7 {
		return nil, errors.New("rarfs: unreadable first volume")
	}
	byName := map[string]*Entry{}
	var order []string
	var walk func(Volume, int, map[string]*Entry, *[]string) error
	switch {
	case len(head) >= 8 && string(head[:8]) == string(rar5Sig):
		walk = walkRar5
	case string(head[:7]) == string(rar4Sig):
		walk = walkRar4
	default:
		return nil, errors.New("rarfs: not a rar archive")
	}
	for i, v := range vols {
		if err := walk(v, i, byName, &order); err != nil {
			return nil, err
		}
	}
	a := &Archive{}
	for _, n := range order {
		a.Entries = append(a.Entries, *byName[n])
	}
	return a, nil
}

func addFrag(byName map[string]*Entry, order *[]string, name string, size int64, streamable bool, f frag) {
	e := byName[name]
	if e == nil {
		e = &Entry{Name: name, Size: size, Streamable: streamable}
		byName[name] = e
		*order = append(*order, name)
	}
	if size > e.Size {
		e.Size = size
	}
	e.frags = append(e.frags, f)
}

type entryReader struct {
	e    *Entry
	vols []Volume
}

func (e *Entry) ReaderAt(vols []Volume) io.ReaderAt { return &entryReader{e: e, vols: vols} }

func (r *entryReader) ReadAt(p []byte, off int64) (int, error) {
	total := 0
	for total < len(p) {
		o := off + int64(total)
		var base int64
		hit := false
		for _, f := range r.e.frags {
			if o < base+f.length {
				want := int(base + f.length - o)
				if want > len(p)-total {
					want = len(p) - total
				}
				n, err := r.vols[f.vol].ReadAt(p[total:total+want], f.off+(o-base))
				total += n
				if err != nil && !errors.Is(err, io.EOF) {
					return total, err
				}
				hit = true
				break
			}
			base += f.length
		}
		if !hit {
			return total, io.EOF
		}
	}
	return total, nil
}

func readSlice(v Volume, off int64, n int) ([]byte, error) {
	buf := make([]byte, n)
	m, err := v.ReadAt(buf, off)
	if m > 0 {
		return buf[:m], nil
	}
	if err == nil {
		err = io.EOF
	}
	return nil, err
}

func uvint(b []byte, pos int) (uint64, int, bool) {
	var val uint64
	var shift uint
	for i := 0; pos+i < len(b) && i < 10; i++ {
		c := b[pos+i]
		val |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			return val, pos + i + 1, true
		}
		shift += 7
	}
	return 0, pos, false
}
