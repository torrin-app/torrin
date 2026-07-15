package nzb

import (
	"strings"
	"testing"
)

func TestGenerateRoundTrips(t *testing.T) {
	out := Generate([]OutFile{{
		Subject: "deadbeef", Name: "cafe1234", Group: "alt.binaries.torrin",
		Segments: []Segment{
			{MessageID: "seg1@torrin", Number: 1, Bytes: 768000},
			{MessageID: "seg2@torrin", Number: 2, Bytes: 100},
		},
	}})

	parsed, err := ParseBytes(out)
	if err != nil {
		t.Fatalf("generated nzb does not parse: %v", err)
	}
	if len(parsed.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(parsed.Files))
	}
	f := parsed.Files[0]
	if len(f.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(f.Segments))
	}
	if f.Segments[0].MessageID != "seg1@torrin" || f.Segments[1].MessageID != "seg2@torrin" {
		t.Errorf("message-ids drifted: %+v", f.Segments)
	}
	if len(f.Groups) != 1 || f.Groups[0] != "alt.binaries.torrin" {
		t.Errorf("groups = %v", f.Groups)
	}
}

func TestGenerateEscapesXML(t *testing.T) {
	out := string(Generate([]OutFile{{
		Subject: "a&b", Name: "x<y>", Group: "g", Segments: []Segment{{MessageID: "a&b@x", Number: 1, Bytes: 1}},
	}}))
	if strings.Contains(out, "a&b@x") && !strings.Contains(out, "a&amp;b@x") {
		t.Error("message-id not xml-escaped")
	}
}

func TestGenerateMultiFile(t *testing.T) {
	out := Generate([]OutFile{
		{Subject: "s1", Name: "n1", Group: "g", Segments: []Segment{{MessageID: "m1", Number: 1, Bytes: 1}}},
		{Subject: "s2", Name: "n2", Group: "g", Segments: []Segment{{MessageID: "m2", Number: 1, Bytes: 1}}},
	})
	parsed, err := ParseBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(parsed.Files))
	}
}
