package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/storage"
	sharedusenet "github.com/torrin-app/torrin/shared/usenet"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
	"github.com/torrin-app/torrin/shared/video"
)

const (
	cairnWarmHead = 1 << 20 // header bytes to pre-pull on open
	cairnWarmTail = 2 << 20 // trailing bytes (MKV cues) to pre-pull on open
)

func (s *Server) serveCairn(w http.ResponseWriter, r *http.Request, key string) {
	hash, index, requestedName, ok := cairn.ParseStreamPath(key)
	if !ok {
		httpError(w, 404, "not found")
		return
	}
	if s.cairnStore == nil || s.cairnFetch == nil {
		httpError(w, 503, "cairn streaming unavailable")
		return
	}
	if r.Method == http.MethodGet {
		identity := cairnStreamIdentity(r)
		switch s.cairnSlots.acquire(identity) {
		case streamLimitUser:
			w.Header().Set("Retry-After", "5")
			httpError(w, http.StatusTooManyRequests, "cairn user stream limit reached")
			return
		case streamLimitTotal:
			w.Header().Set("Retry-After", "5")
			httpError(w, http.StatusServiceUnavailable, "cairn stream capacity reached")
			return
		default:
			defer s.cairnSlots.release(identity)
		}
	}
	data, err := s.cairnStore.GetBytes(r.Context(), nzb.StorageKey(hash))
	if err != nil {
		s.notFound(w, r, key, err)
		return
	}
	parsed, err := nzb.ParseBytes(data)
	if err != nil {
		s.notFound(w, r, key, fmt.Errorf("invalid cairn nzb: %w", err))
		return
	}
	if index >= len(parsed.Files) {
		s.notFound(w, r, key, fmt.Errorf("invalid cairn file index %d", index))
		return
	}
	file := parsed.Files[index]
	name := file.Filename
	if name == "" {
		name = file.Subject
	}
	name = filepath.Base(name)
	if name != requestedName {
		httpError(w, 404, "not found")
		return
	}
	reader, err := sharedusenet.NewReader(r.Context(), file, s.cairnFetch)
	if err != nil {
		s.notFound(w, r, key, err)
		return
	}
	encrypted := r.URL.Query().Get("enc") == "1"
	if encrypted && s.cipher == nil {
		httpError(w, http.StatusServiceUnavailable, "encrypted cairn streaming unavailable")
		return
	}
	if r.Method == http.MethodGet {
		offset, probe := s.cairnProbeOffset(r, reader, encrypted)
		if probe {
			var first [1]byte
			if _, err := reader.ReadAt(first[:], offset); err != nil {
				slog.Warn("stream: cairn article preflight failed", "key", key,
					"range", r.Header.Get("Range"), "offset", offset, "err", err)
				httpError(w, http.StatusBadGateway, "cairn article unavailable")
				return
			}
		}
		// On the opening read (offset 0) pre-pull the header and trailing index so
		// the player's first seek, which reads the MKV cues at the end, is warm.
		// Seeks (offset > 0) skip this so they don't re-warm on every jump.
		if offset == 0 {
			reader.Warm(0, cairnWarmHead)
			reader.Warm(reader.Size()-cairnWarmTail, cairnWarmTail)
		}
	}

	h := w.Header()
	h.Set("Content-Type", video.ContentType(name))
	h.Set("Cache-Control", "no-store")
	logged := loggedCairnReaderAt{reader: reader, key: key}
	if encrypted {
		s.serveEncrypted(w, r, key, readerEncryptedSource{reader: logged, size: reader.Size()})
		return
	}
	s.setDownloadDisposition(w, r, key)
	s.recordView(r, key)
	http.ServeContent(w, r, name, time.Time{}, io.NewSectionReader(logged, 0, reader.Size()))
}

func (s *Server) cairnProbeOffset(r *http.Request, reader *sharedusenet.Reader, encrypted bool) (int64, bool) {
	total := reader.Size()
	if encrypted {
		plainTotal, err := s.cipher.PlainSize(total)
		if err != nil {
			return 0, false
		}
		total = plainTotal
	}
	if r.Header.Get("Range") == "" {
		return 0, true
	}
	start, end, ok := storage.ParseRange(r.Header.Get("Range"), total)
	if !ok {
		return 0, false
	}
	if !encrypted {
		return start, true
	}
	plan, err := s.cipher.PlanRange(start, end+1, total)
	if err != nil {
		return 0, false
	}
	return plan.EncStart, true
}

type loggedCairnReaderAt struct {
	reader *sharedusenet.Reader
	key    string
}

func (r loggedCairnReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.reader.ReadAt(p, off)
	if err != nil && err != io.EOF {
		slog.Warn("stream: cairn read failed", "key", r.key, "offset", off, "err", err)
	}
	return n, err
}

type readerEncryptedSource struct {
	reader io.ReaderAt
	size   int64
}

func (s readerEncryptedSource) Head(context.Context) (*storage.Object, error) {
	return &storage.Object{Size: s.size}, nil
}

func (s readerEncryptedSource) Get(_ context.Context, rng string) (*storage.Object, error) {
	start, end := int64(0), s.size-1
	if rng != "" {
		var ok bool
		start, end, ok = storage.ParseRange(rng, s.size)
		if !ok {
			return nil, fmt.Errorf("invalid encrypted range %q", rng)
		}
	}
	length := end - start + 1
	return &storage.Object{
		Body:         io.NopCloser(io.NewSectionReader(s.reader, start, length)),
		Size:         length,
		ContentRange: fmt.Sprintf("bytes %d-%d/%d", start, end, s.size),
	}, nil
}
