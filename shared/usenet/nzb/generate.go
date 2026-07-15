package nzb

import (
	"bytes"
	"fmt"
	"html"
)

type OutFile struct {
	Subject  string
	Name     string
	Group    string
	Segments []Segment
}

func Generate(files []OutFile) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">` + "\n")
	for _, f := range files {
		subj := fmt.Sprintf("%s [1/1] - \"%s\" yEnc (1/%d)", f.Subject, f.Name, len(f.Segments))
		fmt.Fprintf(&b, "  <file poster=\"torrin\" subject=\"%s\">\n", html.EscapeString(subj))
		b.WriteString("    <groups>\n")
		fmt.Fprintf(&b, "      <group>%s</group>\n", html.EscapeString(f.Group))
		b.WriteString("    </groups>\n")
		b.WriteString("    <segments>\n")
		for _, s := range f.Segments {
			fmt.Fprintf(&b, "      <segment bytes=\"%d\" number=\"%d\">%s</segment>\n",
				s.Bytes, s.Number, html.EscapeString(s.MessageID))
		}
		b.WriteString("    </segments>\n")
		b.WriteString("  </file>\n")
	}
	b.WriteString("</nzb>\n")
	return b.Bytes()
}
