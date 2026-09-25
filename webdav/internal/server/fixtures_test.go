package server

import (
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
)

func file(idx int, name string, size int64) jobs.File {
	return jobs.File{Index: idx, Name: name, Size: size}
}

func job(name, hash string, files ...jobs.File) *jobs.Job {
	return &jobs.Job{Name: name, InfoHash: hash, Status: jobs.StatusComplete, UpdatedAt: time.Unix(1700000000, 0), Files: files}
}

func sample() []*jobs.Job {
	return []*jobs.Job{
		job("Alpha 2020", "aaaaaaaa1111", file(0, "alpha.mkv", 100)),
		job("Beta S01", "bbbbbbbb2222", file(0, "pack/S01E01.mkv", 200), file(1, "pack/S01E01.mkv", 210)),
		job("Alpha 2020", "cccccccc3333", file(0, "alpha.mkv", 50)),
		{Name: "Pending", InfoHash: "dddd", Status: jobs.StatusDownloading, Files: []jobs.File{file(0, "x.mkv", 1)}},
	}
}

func names(n *node) []string {
	var out []string
	for _, c := range n.children {
		out = append(out, c.name)
	}
	return out
}
