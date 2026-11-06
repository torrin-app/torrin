package nzb

import "testing"

const decimalBytesNZB = `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <file poster="x" date="1" subject="[1/1] - &quot;ep.mkv&quot; yEnc (1/2)">
    <groups><group>a.b.test</group></groups>
    <segments>
      <segment bytes="17915735670.25" number="1">m1@x</segment>
      <segment bytes="739000" number="2">m2@x</segment>
    </segments>
  </file>
</nzb>`

func TestParseTolerantesDecimalBytes(t *testing.T) {
	n, err := ParseBytes([]byte(decimalBytesNZB))
	if err != nil {
		t.Fatalf("decimal byte count should parse, got: %v", err)
	}
	if len(n.Files) != 1 || len(n.Files[0].Segments) != 2 {
		t.Fatalf("unexpected parse: %+v", n.Files)
	}
	if got := n.Files[0].Segments[0].Bytes; got != 17915735670 {
		t.Errorf("segment bytes = %d, want truncated 17915735670", got)
	}
}

func TestParseToleratesDecimalDate(t *testing.T) {
	// Mad Dog S01: poster emitted date="17915735670.25" (decimal) which the
	// parser rejects on ParseInt even though the byte counts are clean.
	nzbText := `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <file poster="" date="17915735670.25" subject="[1/1] - &quot;ep.mkv&quot; yEnc (1/1)">
    <groups><group>a.b.test</group></groups>
    <segments><segment bytes="739000" number="1">m@x</segment></segments>
  </file>
</nzb>`
	n, err := ParseBytes([]byte(nzbText))
	if err != nil {
		t.Fatalf("decimal date should parse, got: %v", err)
	}
	if len(n.Files) != 1 {
		t.Fatalf("unexpected parse: %+v", n.Files)
	}
}

func TestTruncateDecimalAttrNoopWhenClean(t *testing.T) {
	clean := `<segment bytes="123" number="1">m@x</segment>`
	if truncateDecimalAttr([]byte(clean), "bytes") != nil {
		t.Error("should return nil (no change) when there are no decimal byte counts")
	}
	got := truncateDecimalAttr([]byte(`bytes="12.9" bytes="30.0"`), "bytes")
	if string(got) != `bytes="12" bytes="30"` {
		t.Errorf("got %q", got)
	}
	if string(truncateDecimalAttr([]byte(`date="99.5"`), "date")) != `date="99"` {
		t.Error("date decimals should truncate too")
	}
}

func TestTotalSizeAndName(t *testing.T) {
	n := &NZB{
		Meta:  map[string]string{"name": "My.Release"},
		Files: []File{{Segments: []Segment{{Bytes: 100}, {Bytes: 50}}}},
	}
	if n.TotalSize() != 150 {
		t.Errorf("total = %d, want 150", n.TotalSize())
	}
	if n.Name() != "My.Release" {
		t.Errorf("name = %q", n.Name())
	}
}

func TestHashStable(t *testing.T) {
	a := &NZB{Files: []File{{Segments: []Segment{{MessageID: "b"}, {MessageID: "a"}}}}}
	b := &NZB{Files: []File{{Segments: []Segment{{MessageID: "a"}, {MessageID: "b"}}}}}
	if Hash(a) != Hash(b) {
		t.Error("hash should be order-independent")
	}
	if len(Hash(a)) != 40 {
		t.Errorf("hash length = %d, want 40", len(Hash(a)))
	}
}
