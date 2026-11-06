package download

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/Tensai75/nntpPool"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
	"github.com/torrin-app/torrin/shared/usenet/par2"
	"github.com/torrin-app/torrin/shared/usenet/rarfs"
)

type volCand struct {
	ra   *FileReaderAt
	name string
}

func orderVolumes(ctx context.Context, pools []nntpPool.ConnectionPool, n *nzb.NZB) ([]rarfs.Volume, error) {
	var cands []volCand
	var par2Files []nzb.File
	for _, f := range n.Files {
		name := strings.ToLower(FileName(f))
		if strings.Contains(name, ".par2") {
			par2Files = append(par2Files, f)
			continue
		}
		if isOptional(name) && !isRarName(name) {
			continue
		}
		cands = append(cands, volCand{ra: NewFileReaderAt(ctx, pools, f, groupOf(f)), name: FileName(f)})
	}
	if len(cands) == 0 {
		return nil, errors.New("usenet: no archive volumes")
	}
	if needsDeobf(cands) && len(par2Files) > 0 {
		deobfuscate(ctx, pools, cands, par2Files)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return rarVolNum(cands[i].name) < rarVolNum(cands[j].name)
	})
	vols := make([]rarfs.Volume, len(cands))
	for i := range cands {
		vols[i] = cands[i].ra
	}
	return vols, nil
}

func needsDeobf(cands []volCand) bool {
	for _, c := range cands {
		if isRarName(strings.ToLower(c.name)) {
			return false
		}
	}
	return true
}

func deobfuscate(ctx context.Context, pools []nntpPool.ConnectionPool, cands []volCand, par2Files []nzb.File) {
	small := par2Files[0]
	for _, f := range par2Files[1:] {
		if fileBytes(f) < fileBytes(small) {
			small = f
		}
	}
	data := readAll(ctx, pools, small)
	names := map[string]string{}
	par2.FileDescs(data, names)
	if len(names) == 0 {
		return
	}
	parallelDo(len(cands), openConcurrency, func(i int) {
		head := make([]byte, 64*1024)
		nn, _ := cands[i].ra.ReadAt(head, 0)
		if real := names[par2.Hash16k(bytes.NewReader(head[:nn]))]; real != "" {
			cands[i].name = real
		}
	})
}

func readAll(ctx context.Context, pools []nntpPool.ConnectionPool, f nzb.File) []byte {
	ra := NewFileReaderAt(ctx, pools, f, groupOf(f))
	size, err := ra.Size()
	if err != nil || size <= 0 || size > 64*1024*1024 {
		return nil
	}
	buf := make([]byte, size)
	n, _ := ra.ReadAt(buf, 0)
	return buf[:n]
}

func fileBytes(f nzb.File) int64 {
	var t int64
	for _, s := range f.Segments {
		t += s.Bytes
	}
	return t
}

func rarVolNum(name string) int {
	n := strings.ToLower(name)
	if i := strings.LastIndex(n, ".part"); i >= 0 {
		return digitsAt(n, i+5)
	}
	if strings.HasSuffix(n, ".rar") {
		return 0
	}
	if m := len(n); m >= 4 && n[m-4] == '.' && n[m-3] == 'r' && isDigit(n[m-2]) && isDigit(n[m-1]) {
		return 1 + int(n[m-2]-'0')*10 + int(n[m-1]-'0')
	}
	return 1 << 20
}

func digitsAt(s string, i int) int {
	num := 0
	for i < len(s) && isDigit(s[i]) {
		num = num*10 + int(s[i]-'0')
		i++
	}
	return num
}
