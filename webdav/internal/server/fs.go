package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/net/webdav"

	"github.com/torrin-app/torrin/shared/video"
)

var errReadOnly = errors.New("read-only")

type davFS struct{ root *node }

func segments(name string) []string {
	name = strings.Trim(path.Clean("/"+name), "/")
	if name == "" {
		return nil
	}
	return strings.Split(name, "/")
}

func (d davFS) node(name string) (*node, error) {
	cur := d.root
	for _, seg := range segments(name) {
		if cur == nil || !cur.dir {
			return nil, os.ErrNotExist
		}
		cur = cur.index[seg]
		if cur == nil || cur.hidden {
			return nil, os.ErrNotExist
		}
	}
	return cur, nil
}

func (d davFS) OpenFile(_ context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE) != 0 {
		return nil, errReadOnly
	}
	n, err := d.node(name)
	if err != nil {
		return nil, err
	}
	return &davFile{n: n}, nil
}

func (d davFS) Stat(_ context.Context, name string) (os.FileInfo, error) {
	n, err := d.node(name)
	if err != nil {
		return nil, err
	}
	return davInfo{n: n}, nil
}

func (davFS) Mkdir(context.Context, string, os.FileMode) error { return errReadOnly }
func (davFS) RemoveAll(context.Context, string) error          { return errReadOnly }
func (davFS) Rename(context.Context, string, string) error     { return errReadOnly }

type davFile struct {
	n   *node
	pos int
}

func (*davFile) Close() error                   { return nil }
func (*davFile) Read([]byte) (int, error)       { return 0, io.EOF }
func (*davFile) Write([]byte) (int, error)      { return 0, errReadOnly }
func (*davFile) Seek(int64, int) (int64, error) { return 0, nil }
func (f *davFile) Stat() (os.FileInfo, error)   { return davInfo{n: f.n}, nil }

func (f *davFile) Readdir(count int) ([]os.FileInfo, error) {
	if !f.n.dir {
		return nil, os.ErrInvalid
	}
	kids := make([]*node, 0, len(f.n.children))
	for _, c := range f.n.children {
		if !c.hidden {
			kids = append(kids, c)
		}
	}
	if f.pos >= len(kids) {
		if count > 0 {
			return nil, io.EOF
		}
		return nil, nil
	}
	end := len(kids)
	if count > 0 && f.pos+count < end {
		end = f.pos + count
	}
	out := make([]os.FileInfo, 0, end-f.pos)
	for _, c := range kids[f.pos:end] {
		out = append(out, davInfo{n: c})
	}
	f.pos = end
	return out, nil
}

type davInfo struct{ n *node }

func (i davInfo) Name() string {
	if i.n.name == "" {
		return "/"
	}
	return i.n.name
}
func (i davInfo) Size() int64        { return i.n.size }
func (i davInfo) ModTime() time.Time { return i.n.mod }
func (i davInfo) IsDir() bool        { return i.n.dir }
func (davInfo) Sys() any             { return nil }

func (i davInfo) Mode() os.FileMode {
	if i.n.dir {
		return os.ModeDir | 0o555
	}
	return 0o444
}

func (i davInfo) ETag(context.Context) (string, error) {
	if i.n.etag == "" {
		return "", webdav.ErrNotImplemented
	}
	return i.n.etag, nil
}

func (i davInfo) ContentType(context.Context) (string, error) {
	if i.n.dir {
		return "", webdav.ErrNotImplemented
	}
	if ct := video.ContentType(i.n.name); ct != "" {
		return ct, nil
	}
	return "", webdav.ErrNotImplemented
}
