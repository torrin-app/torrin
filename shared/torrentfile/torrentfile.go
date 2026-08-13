package torrentfile

import (
	"bytes"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
)

type File struct {
	Path string
	Size int64
}

type Meta struct {
	InfoHash string
	Name     string
	Private  bool
	Size     int64
	Files    []File
}

func Parse(data []byte) (*Meta, error) {
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, err
	}
	m := &Meta{
		InfoHash: mi.HashInfoBytes().HexString(),
		Name:     info.Name,
		Private:  info.Private != nil && *info.Private,
		Size:     info.TotalLength(),
	}
	for _, f := range info.UpvertedFiles() {
		p := strings.Join(f.Path, "/")
		if p == "" {
			p = info.Name
		}
		m.Files = append(m.Files, File{Path: p, Size: f.Length})
	}
	return m, nil
}

func XSeedKey(infoHash string) string { return "xseed/" + infoHash + ".torrent" }

func (m *Meta) Sizes() []int64 {
	out := make([]int64, len(m.Files))
	for i, f := range m.Files {
		out[i] = f.Size
	}
	return out
}

func SubsetBySize(want, have []int64) bool {
	if len(want) == 0 {
		return false
	}
	counts := map[int64]int{}
	for _, s := range have {
		counts[s]++
	}
	for _, s := range want {
		if counts[s] == 0 {
			return false
		}
		counts[s]--
	}
	return true
}
