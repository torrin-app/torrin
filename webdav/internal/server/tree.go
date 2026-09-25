package server

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/foldername"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
)

type node struct {
	name     string
	orig     string
	alias    string
	idx      int
	hidden   bool
	dir      bool
	size     int64
	mod      time.Time
	etag     string
	key      string
	hash     string
	node     string
	enc      bool
	cairn    bool
	children []*node
	index    map[string]*node
}

func newDir(name string) *node {
	return &node{name: name, dir: true, index: map[string]*node{}}
}

func (n *node) add(c *node) {
	n.children = append(n.children, c)
	n.index[c.name] = c
}

func (n *node) find(segs []string) *node {
	cur := n
	for _, s := range segs {
		if cur == nil || !cur.dir {
			return nil
		}
		cur = cur.index[s]
	}
	return cur
}

func buildTree(list []*jobs.Job, overrides map[string]auth.WebdavOverride) *node {
	root := newDir("")
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].InfoHash < list[j].InfoHash
	})
	folders := map[string]bool{}
	for _, j := range list {
		if j.Status != jobs.StatusComplete || len(j.Files) == 0 {
			continue
		}
		folder := newDir(foldername.Unique(folders, foldername.Of(j), foldername.Short(j.InfoHash)))
		folder.mod = j.UpdatedAt
		folder.hash = j.InfoHash
		folder.idx = -1
		folder.hidden = overrides[auth.WebdavKey(j.InfoHash, -1)].Excluded
		names := map[string]bool{}
		for i, f := range j.Files {
			ov := overrides[auth.WebdavKey(j.InfoHash, i)]
			orig := base(f.Name)
			display := orig
			if ov.Alias != "" {
				display = ov.Alias
			}
			folder.add(&node{
				name:   foldername.Unique(names, display, strconv.Itoa(i)),
				orig:   orig,
				alias:  ov.Alias,
				idx:    i,
				hidden: ov.Excluded,
				size:   f.Size,
				mod:    j.UpdatedAt,
				key:    manifest.ResolveKey(j.InfoHash, i, f.Key, f.Name),
				hash:   j.InfoHash,
				node:   j.Node,
				enc:    f.Enc,
				etag:   `"` + j.InfoHash + "-" + strconv.Itoa(i) + "-" + strconv.FormatInt(f.Size, 10) + `"`,
			})
		}
		root.add(folder)
	}
	return root
}

func base(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}
