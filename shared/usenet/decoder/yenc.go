package decoder

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
)

// ErrChecksumMismatch reports that a segment's decoded bytes do not match the
// checksum the poster recorded in the =yend trailer. The article arrived, but
// what came back is not what was posted.
var ErrChecksumMismatch = errors.New("yenc: checksum mismatch")

type YencResult struct {
	Data     []byte
	Filename string
	Part     int
	Total    int
	Size     int64
	Begin    int64
	End      int64
}

func Decode(data []byte) (*YencResult, error) {
	result := &YencResult{}

	normalized := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	lines := bytes.Split(normalized, []byte("\n"))

	var dataStart, dataEnd int
	var yend string
	foundBegin := false
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("=ybegin ")) {
			parseYencHeader(string(line), result)
			dataStart = i + 1
			foundBegin = true
		} else if bytes.HasPrefix(line, []byte("=ypart ")) {
			parseYpartHeader(string(line), result)
			dataStart = i + 1
		} else if bytes.HasPrefix(line, []byte("=yend ")) {
			yend = string(line)
			dataEnd = i
			break
		}
	}

	if !foundBegin {
		return nil, fmt.Errorf("no yenc data found")
	}
	if dataEnd == 0 {
		dataEnd = len(lines)
	}
	if dataStart >= dataEnd {
		result.Data = []byte{}
	} else {
		var buf bytes.Buffer
		const maxGrow = 2 * 1024 * 1024
		if result.Size > 0 && result.Size <= maxGrow {
			buf.Grow(int(result.Size))
		}
		for _, line := range lines[dataStart:dataEnd] {
			decodeLine(line, &buf)
		}
		result.Data = buf.Bytes()
	}

	if err := verifyChecksum(yend, result); err != nil {
		return nil, err
	}
	return result, nil
}

// verifyChecksum checks the decoded part against the checksum in the =yend
// trailer, when the poster supplied one that covers these bytes.
//
// pcrc32 always covers the part in hand, so it is checked whenever present. A
// bare crc32 covers the whole file, so it only applies here when the post is
// single-part, checking it against one part of a multipart post would reject
// every segment.
//
// Posters vary in what they emit. An absent or unparseable checksum is not an
// error; it just means there is nothing to verify.
func verifyChecksum(yend string, r *YencResult) error {
	if yend == "" {
		return nil
	}
	pcrc, crc := yendChecksums(yend)
	want := pcrc
	if want == "" {
		if !singlePart(r) {
			return nil
		}
		want = crc
	}
	if want == "" {
		return nil
	}
	sum, err := strconv.ParseUint(want, 16, 32)
	if err != nil {
		return nil
	}
	if got := crc32.ChecksumIEEE(r.Data); uint32(sum) != got {
		return fmt.Errorf("%w: part %d: want %08X, got %08X", ErrChecksumMismatch, r.Part, uint32(sum), got)
	}
	return nil
}

// singlePart reports whether a bare =yend crc32 covers exactly the bytes
// decoded here. A post with no part= header is single-part by definition;
// part=1 total=1 is the same thing spelled out.
func singlePart(r *YencResult) bool {
	return r.Part == 0 || (r.Part == 1 && r.Total <= 1)
}

// yendChecksums pulls the checksum fields out of a =yend trailer, matching
// whole key tokens: a substring search for "crc32=" also finds the tail of
// "pcrc32=", which would read a part checksum as a whole-file one.
func yendChecksums(line string) (pcrc, crc string) {
	for _, tok := range strings.Fields(line) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(k) {
		case "pcrc32":
			pcrc = v
		case "crc32":
			crc = v
		}
	}
	return pcrc, crc
}

func decodeLine(line []byte, buf *bytes.Buffer) {
	i := 0
	for i < len(line) {
		b := line[i]
		if b == '=' && i+1 < len(line) {
			i++
			b = line[i] - 64
		}
		buf.WriteByte(b - 42)
		i++
	}
}

func parseYencHeader(line string, r *YencResult) {
	if v := extractValue(line, "name"); v != "" {
		r.Filename = v
	}
	if v := extractValue(line, "size"); v != "" {
		r.Size, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := extractValue(line, "part"); v != "" {
		r.Part, _ = strconv.Atoi(v)
	}
	if v := extractValue(line, "total"); v != "" {
		r.Total, _ = strconv.Atoi(v)
	}
}

func parseYpartHeader(line string, r *YencResult) {
	if v := extractValue(line, "begin"); v != "" {
		r.Begin, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := extractValue(line, "end"); v != "" {
		r.End, _ = strconv.ParseInt(v, 10, 64)
	}
}

func extractValue(line, key string) string {
	prefix := key + "="
	idx := strings.Index(line, prefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(prefix)
	if key == "name" {
		return strings.TrimSpace(line[start:])
	}
	end := strings.IndexByte(line[start:], ' ')
	if end < 0 {
		return strings.TrimSpace(line[start:])
	}
	return line[start : start+end]
}
