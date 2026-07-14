package jobs

import "time"

type Status string

const (
	StatusPending     Status = "pending"
	StatusQueued      Status = "queued"
	StatusDownloading Status = "downloading"
	StatusProcessing  Status = "processing"
	StatusPublishing  Status = "publishing"
	StatusComplete    Status = "complete"
	StatusFailed      Status = "failed"
	StatusEvicted     Status = "evicted"
)

var activeStatuses = []Status{StatusPending, StatusQueued, StatusDownloading, StatusProcessing, StatusPublishing}

func (s Status) Active() bool {
	for _, a := range activeStatuses {
		if s == a {
			return true
		}
	}
	return false
}

var activeStates = sqlInList(activeStatuses)

var downloadingStatuses = []Status{StatusPending, StatusDownloading, StatusProcessing, StatusPublishing}
var downloadingStates = sqlInList(downloadingStatuses)

func sqlInList(ss []Status) string {
	out := "("
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += "'" + string(s) + "'"
	}
	return out + ")"
}

type Source string

const (
	SourceTorrent  Source = "torrent"
	SourceUsenet   Source = "usenet"
	SourceHoster   Source = "hoster"
	SourceHDEncode Source = "hdencode"
	SourceScenerls Source = "scenerls"
	SourceTelegram Source = "telegram"
	SourceYtdlp    Source = "ytdlp"
)

type Job struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id,omitempty"`
	InfoHash     string    `json:"info_hash"`
	Name         string    `json:"name"`
	Magnet       string    `json:"magnet,omitempty"`
	Source       Source    `json:"source"`
	Status       Status    `json:"status"`
	Error        string    `json:"error,omitempty"`
	Files        []File    `json:"files,omitempty"`
	SelectedIdxs []int     `json:"selected_indexes,omitempty"`
	IMDBID       string    `json:"imdb_id,omitempty"`
	FileSize     int64     `json:"file_size"`
	MaxBytes     int64     `json:"max_bytes"`
	Priority     int       `json:"priority"`
	Progress     float64   `json:"progress,omitempty"`
	Speed        int64     `json:"speed,omitempty"`
	Node         string    `json:"node,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	StreamURLs   []Stream  `json:"stream_urls,omitempty"`
}

type File struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
}

type Stream struct {
	FileName  string `json:"file_name"`
	Size      int64  `json:"size,omitempty"`
	SignedURL string `json:"signed_url,omitempty"`
}
