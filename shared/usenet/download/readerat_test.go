package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/usenet/decoder"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

func TestFileReaderAtPrefetch(t *testing.T) {
	chunk := 9000
	payload := make([]byte, 300000)
	for i := range payload {
		payload[i] = byte((i*7+13)%255 + 1)
	}
	file, base := fakeFile(payload, chunk)
	var mu sync.Mutex
	fetched := map[int]bool{}
	fetch := func(ctx context.Context, seg nzb.Segment) (*decoder.YencResult, error) {
		var off int
		fmt.Sscanf(seg.MessageID, "s%d", &off)
		mu.Lock()
		fetched[off/chunk] = true
		mu.Unlock()
		return base(ctx, seg)
	}
	r := newReaderAt(context.Background(), file, fetch)
	if _, err := r.ReadAt(make([]byte, 100), 0); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := 0
		for i := 1; i <= 5; i++ {
			if fetched[i] {
				n++
			}
		}
		mu.Unlock()
		if n >= 5 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("prefetch did not warm the upcoming segments")
}

func holeyFile(payload []byte, chunk int, missing map[int]bool) (nzb.File, segFetch) {
	file, base := fakeFile(payload, chunk)
	fetch := func(ctx context.Context, seg nzb.Segment) (*decoder.YencResult, error) {
		var off int
		fmt.Sscanf(seg.MessageID, "s%d", &off)
		if missing[off/chunk] {
			return nil, fmt.Errorf("430 no such article")
		}
		return base(ctx, seg)
	}
	return file, fetch
}

func TestFileReaderAtZeroFillsHoles(t *testing.T) {
	chunk := 9000
	payload := make([]byte, 90000)
	for i := range payload {
		payload[i] = byte((i*7+13)%255 + 1)
	}
	file, fetch := holeyFile(payload, chunk, map[int]bool{3: true, 5: true})
	r := newReaderAt(context.Background(), file, fetch)

	all := make([]byte, len(payload))
	n, err := r.ReadAt(all, 0)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("short read n=%d want %d", n, len(payload))
	}
	want := append([]byte(nil), payload...)
	for _, s := range []int{3, 5} {
		for i := s * chunk; i < (s+1)*chunk; i++ {
			want[i] = 0
		}
	}
	if !bytes.Equal(all, want) {
		t.Fatal("hole zero-fill mismatch or misaligned")
	}
}

func TestFileReaderAtFailsWhenTooDamaged(t *testing.T) {
	chunk := 9000
	payload := make([]byte, 90000)
	file, fetch := holeyFile(payload, chunk, map[int]bool{2: true, 3: true, 4: true, 5: true, 6: true})
	r := newReaderAt(context.Background(), file, fetch)
	if _, err := r.ReadAt(make([]byte, len(payload)), 0); !errors.Is(err, ErrTooManyHoles) {
		t.Fatalf("want ErrTooManyHoles for 5 consecutive missing, got %v", err)
	}
}

func fakeFile(payload []byte, chunk int) (nzb.File, segFetch) {
	var segs []nzb.Segment
	for off := 0; off < len(payload); off += chunk {
		segs = append(segs, nzb.Segment{MessageID: fmt.Sprintf("s%d", off)})
	}
	fetch := func(ctx context.Context, seg nzb.Segment) (*decoder.YencResult, error) {
		var off int
		fmt.Sscanf(seg.MessageID, "s%d", &off)
		end := off + chunk
		if end > len(payload) {
			end = len(payload)
		}
		return &decoder.YencResult{Data: append([]byte(nil), payload[off:end]...), Begin: int64(off) + 1, End: int64(end)}, nil
	}
	return nzb.File{Segments: segs}, fetch
}

func TestFileReaderAt(t *testing.T) {
	payload := make([]byte, 100000)
	for i := range payload {
		payload[i] = byte((i*7 + 13) % 256)
	}
	file, fetch := fakeFile(payload, 9000)
	r := newReaderAt(context.Background(), file, fetch)

	if sz, err := r.Size(); err != nil || sz != int64(len(payload)) {
		t.Fatalf("Size = %d, %v", sz, err)
	}
	for _, off := range []int64{0, 1, 8999, 9000, 9001, 50000, 99999} {
		buf := make([]byte, 5000)
		n, err := r.ReadAt(buf, off)
		if err != nil && err != io.EOF {
			t.Fatalf("ReadAt(%d): %v", off, err)
		}
		want := payload[off:]
		if len(want) > 5000 {
			want = want[:5000]
		}
		if !bytes.Equal(buf[:n], want) {
			t.Fatalf("ReadAt(%d) mismatch n=%d", off, n)
		}
	}
	all := make([]byte, len(payload))
	if _, err := r.ReadAt(all, 0); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if !bytes.Equal(all, payload) {
		t.Fatal("full read mismatch")
	}
}
