package server

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/foldername"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

const cairnCacheTTL = 5 * time.Minute

type cairnCache struct {
	mu sync.Mutex
	m  map[string]cairnEntry
}

type cairnEntry struct {
	folders []cairnFolder
	at      time.Time
}

type cairnFolder struct {
	hash, name string
	mod        time.Time
	files      []cairnFile
}

type cairnFile struct {
	name string
	idx  int
	size int64
	enc  bool
}

func newCairnCache() *cairnCache { return &cairnCache{m: map[string]cairnEntry{}} }

func (s *Server) mergeCairns(ctx context.Context, userID string, tree *node, overrides map[string]auth.WebdavOverride) {
	if s.cairnStore == nil {
		return
	}
	cached := map[string]bool{}
	names := map[string]bool{}
	for _, c := range tree.children {
		cached[c.hash] = true
		names[c.name] = true
	}
	for _, fd := range s.cairnData(ctx, userID) {
		if cached[fd.hash] {
			continue
		}
		fov := overrides[auth.WebdavKey(fd.hash, -1)]
		folderName := fd.name
		if fov.Alias != "" {
			folderName = fov.Alias
		}
		folder := newDir(foldername.Unique(names, folderName, foldername.Short(fd.hash)))
		folder.hash, folder.idx, folder.mod = fd.hash, -1, fd.mod
		folder.hidden = fov.Excluded
		fnames := map[string]bool{}
		for _, f := range fd.files {
			ov := overrides[auth.WebdavKey(fd.hash, f.idx)]
			display := f.name
			if ov.Alias != "" {
				display = ov.Alias
			}
			folder.add(&node{
				name:   foldername.Unique(fnames, display, strconv.Itoa(f.idx)),
				orig:   f.name,
				alias:  ov.Alias,
				idx:    f.idx,
				hidden: ov.Excluded,
				size:   f.size,
				mod:    fd.mod,
				cairn:  true,
				enc:    f.enc,
				hash:   fd.hash,
				key:    cairn.StreamPath(fd.hash, f.idx, f.name),
				etag:   `"cairn-` + fd.hash + "-" + strconv.Itoa(f.idx) + `"`,
			})
		}
		tree.add(folder)
	}
}

func (s *Server) cairnData(ctx context.Context, userID string) []cairnFolder {
	s.cairns.mu.Lock()
	if e, ok := s.cairns.m[userID]; ok && time.Since(e.at) < cairnCacheTTL {
		s.cairns.mu.Unlock()
		return e.folders
	}
	s.cairns.mu.Unlock()

	items, _ := s.users.ListUserCairns(ctx, userID)
	var folders []cairnFolder
	seen := map[string]bool{}
	for _, it := range items {
		if !it.Archived || seen[it.InfoHash] {
			continue
		}
		seen[it.InfoHash] = true
		if fd, ok := s.buildCairnFolder(ctx, it); ok {
			folders = append(folders, fd)
		}
	}
	s.cairns.mu.Lock()
	s.cairns.m[userID] = cairnEntry{folders: folders, at: time.Now()}
	s.cairns.mu.Unlock()
	return folders
}

func (s *Server) buildCairnFolder(ctx context.Context, it auth.CairnItem) (cairnFolder, bool) {
	enc := s.cipher != nil
	name := strings.TrimSpace(it.Name)
	if name == "" {
		name = foldername.Short(it.InfoHash)
	}
	if len(it.Files) > 0 {
		fd := cairnFolder{hash: it.InfoHash, name: name, mod: it.CreatedAt}
		for i, f := range it.Files {
			fd.files = append(fd.files, cairnFile{name: base(f.Name), idx: i, size: f.Size, enc: enc})
		}
		return fd, true
	}
	data, ok := s.cairnNZB(ctx, it.InfoHash)
	if !ok {
		return cairnFolder{}, false
	}
	parsed, err := nzb.ParseBytes(data)
	if err != nil || len(parsed.Files) == 0 {
		return cairnFolder{}, false
	}
	fd := cairnFolder{hash: it.InfoHash, name: name, mod: it.CreatedAt}
	for i, file := range parsed.Files {
		fname := base(file.Filename)
		if fname == "" {
			fname = base(file.Subject)
		}
		size := file.Size()
		if enc {
			if ps, e := s.cipher.PlainSize(size); e == nil {
				size = ps
			}
		}
		fd.files = append(fd.files, cairnFile{name: fname, idx: i, size: size, enc: enc})
	}
	return fd, true
}

func (s *Server) cairnNZB(ctx context.Context, hash string) ([]byte, bool) {
	if data, err := s.cairnStore.GetBytes(ctx, nzb.StorageKey(hash)); err == nil && len(data) > 0 {
		return data, true
	}
	return s.users.GetCairnNZB(ctx, hash)
}
