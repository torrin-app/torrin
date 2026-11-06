package nzb

import (
	"bytes"
	"fmt"
	"html"

	"github.com/Tensai75/nzbparser"
)

type NZB struct {
	Files []File
	Meta  map[string]string
	Raw   []byte
}

type File struct {
	Subject  string
	Filename string
	Groups   []string
	Segments []Segment
}

type Segment struct {
	MessageID string
	Number    int
	Bytes     int64
}

func (f File) Size() int64 {
	var total int64
	for _, s := range f.Segments {
		total += s.Bytes
	}
	return total
}

func ParseBytes(data []byte) (*NZB, error) {
	parsed, err := nzbparser.ParseString(string(data))
	if err != nil {
		// Some posters emit decimal values in integer attributes (e.g.
		// bytes="123.45" or date="123.45"), which the parser rejects on
		// ParseInt. Truncate them and retry before giving up.
		if clean := sanitizeDecimalAttrs(data); clean != nil {
			parsed, err = nzbparser.ParseString(string(clean))
		}
	}
	if err != nil {
		return nil, fmt.Errorf("parse nzb: %w", err)
	}
	nzbparser.ScanNzbFile(parsed)

	n := &NZB{Raw: data, Meta: parsed.Meta}
	if n.Meta == nil {
		n.Meta = make(map[string]string)
	}
	for _, f := range parsed.Files {
		file := File{Subject: f.Subject, Filename: f.Filename, Groups: f.Groups}
		for _, s := range f.Segments {
			file.Segments = append(file.Segments, Segment{
				MessageID: html.UnescapeString(s.Id),
				Number:    s.Number,
				Bytes:     int64(s.Bytes),
			})
		}
		n.Files = append(n.Files, file)
	}
	return n, nil
}

// sanitizeDecimalAttrs truncates decimal integer attributes (bytes=, date=) to
// their integer part so a malformed NZB still parses. Returns nil if nothing
// changed (so the caller keeps the original parse error).
func sanitizeDecimalAttrs(data []byte) []byte {
	out := data
	changed := false
	for _, attr := range []string{"bytes", "date"} {
		if c := truncateDecimalAttr(out, attr); c != nil {
			out = c
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return out
}

// truncateDecimalAttr rewrites any attr="<int>.<frac>" to its integer part.
// Returns nil if there is nothing to fix.
func truncateDecimalAttr(data []byte, attr string) []byte {
	needle := []byte(attr + `="`)
	var out []byte
	changed := false
	i := 0
	for {
		j := bytes.Index(data[i:], needle)
		if j < 0 {
			out = append(out, data[i:]...)
			break
		}
		j += i
		valStart := j + len(needle)
		q := bytes.IndexByte(data[valStart:], '"')
		if q < 0 {
			out = append(out, data[i:]...)
			break
		}
		valEnd := valStart + q
		val := data[valStart:valEnd]
		if dot := bytes.IndexByte(val, '.'); dot >= 0 {
			out = append(out, data[i:valStart]...)
			out = append(out, val[:dot]...)
			i = valEnd
			changed = true
		} else {
			out = append(out, data[i:valEnd]...)
			i = valEnd
		}
	}
	if !changed {
		return nil
	}
	return out
}

func (n *NZB) Name() string {
	if name, ok := n.Meta["name"]; ok {
		return name
	}
	if title, ok := n.Meta["title"]; ok {
		return title
	}
	return ""
}

func (n *NZB) TotalSize() int64 {
	var total int64
	for _, f := range n.Files {
		total += f.Size()
	}
	return total
}
