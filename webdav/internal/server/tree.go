package server

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/subosito/gozaru"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
)

type node struct {
	name     string
	dir      bool
	size     int64
	mod      time.Time
	etag     string
	key      string
	hash     string
	node     string
	enc      bool
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

func buildTree(list []*jobs.Job) *node {
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
		folder := newDir(unique(folders, folderName(j), short(j.InfoHash)))
		folder.mod = j.UpdatedAt
		names := map[string]bool{}
		for i, f := range j.Files {
			folder.add(&node{
				name: unique(names, base(f.Name), strconv.Itoa(i)),
				size: f.Size,
				mod:  j.UpdatedAt,
				key:  manifest.ResolveKey(j.InfoHash, i, f.Key, f.Name),
				hash: j.InfoHash,
				node: j.Node,
				enc:  f.Enc,
				etag: `"` + j.InfoHash + "-" + strconv.Itoa(i) + "-" + strconv.FormatInt(f.Size, 10) + `"`,
			})
		}
		root.add(folder)
	}
	return root
}

func unique(taken map[string]bool, name, salt string) string {
	name = gozaru.Sanitize(name)
	if !taken[name] {
		taken[name] = true
		return name
	}
	out := name + " [" + salt + "]"
	for i := 2; taken[out]; i++ {
		out = name + " [" + salt + "-" + strconv.Itoa(i) + "]"
	}
	taken[out] = true
	return out
}

func folderName(j *jobs.Job) string {
	if j.Name != "" {
		return j.Name
	}
	return j.InfoHash
}

func base(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func short(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}
