package download

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/torrin-app/torrin/shared/usenet/nzb"
	"github.com/torrin-app/torrin/shared/usenet/rarfs"
)

type fakeVol struct {
	reads int32
	sized int32
}

func (f *fakeVol) ReadAt(p []byte, _ int64) (int, error) {
	atomic.AddInt32(&f.reads, 1)
	return len(p), nil
}
func (f *fakeVol) Size() (int64, error) { atomic.AddInt32(&f.sized, 1); return 1, nil }

func TestParallelDo(t *testing.T) {
	var sum int64
	parallelDo(100, 8, func(i int) { atomic.AddInt64(&sum, int64(i)) })
	if sum != 4950 {
		t.Fatalf("parallelDo ran incomplete: sum=%d want 4950", sum)
	}
}

func TestPrewarmVolumes(t *testing.T) {
	vols := make([]rarfs.Volume, 5)
	fakes := make([]*fakeVol, 5)
	for i := range vols {
		fakes[i] = &fakeVol{}
		vols[i] = fakes[i]
	}
	prewarmVolumes(vols)
	for i, f := range fakes {
		if atomic.LoadInt32(&f.reads) == 0 || atomic.LoadInt32(&f.sized) == 0 {
			t.Errorf("vol %d not warmed: reads=%d sized=%d", i, f.reads, f.sized)
		}
	}
}

func vidFile(name string) nzb.File {
	return nzb.File{Filename: name, Segments: []nzb.Segment{{MessageID: "m", Bytes: 100}}}
}

func TestStreamable(t *testing.T) {
	cases := []struct {
		name  string
		files []nzb.File
		want  bool
	}{
		{"single mkv", []nzb.File{vidFile("movie.mkv")}, true},
		{"single mp4", []nzb.File{vidFile("movie.mp4")}, true},
		{"single mkv with par2 recovery", []nzb.File{vidFile("movie.mkv"), vidFile("movie.par2"), vidFile("movie.vol00+1.par2")}, true},
		{"rar present", []nzb.File{vidFile("movie.part01.rar"), vidFile("movie.mkv")}, false},
		{"two videos", []nzb.File{vidFile("a.mkv"), vidFile("b.mkv")}, false},
		{"no video", []nzb.File{vidFile("readme.nfo")}, false},
	}
	for _, c := range cases {
		if _, ok := Streamable(&nzb.NZB{Files: c.files}); ok != c.want {
			t.Errorf("%s: Streamable = %v, want %v", c.name, ok, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		files []nzb.File
		want  Kind
	}{
		{"single mkv -> direct", []nzb.File{vidFile("movie.mkv")}, KindDirect},
		{"single mkv with par2 -> direct", []nzb.File{vidFile("movie.mkv"), vidFile("movie.par2")}, KindDirect},
		{"rar set -> archive", []nzb.File{vidFile("movie.part01.rar"), vidFile("movie.part02.rar"), vidFile("movie.par2")}, KindArchive},
		{"r00 volumes -> archive", []nzb.File{vidFile("movie.rar"), vidFile("movie.r00"), vidFile("movie.r01")}, KindArchive},
		{"nfo only -> none", []nzb.File{vidFile("readme.nfo")}, KindNone},
	}
	for _, c := range cases {
		if k, _ := Classify(&nzb.NZB{Files: c.files}); k != c.want {
			t.Errorf("%s: Classify = %v, want %v", c.name, k, c.want)
		}
	}
}

func TestOpenStreamRejectsNonStreamable(t *testing.T) {
	n := &nzb.NZB{Files: []nzb.File{vidFile("readme.nfo")}}
	if _, _, err := OpenStream(context.Background(), nil, n); !errors.Is(err, ErrNotDirectStreamable) {
		t.Fatalf("want ErrNotDirectStreamable, got %v", err)
	}
}
