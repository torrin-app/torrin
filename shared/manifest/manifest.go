package manifest

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/mediainfo"
)

const FileName = "manifest.json"

const zipFile = "all.zip"

func ZipKey(infoHash string) string {
	return infoHash + "/" + zipFile
}

func ZipHash(key string) (string, bool) {
	return strings.CutSuffix(key, "/"+zipFile)
}

type Manifest struct {
	InfoHash  string    `json:"info_hash"`
	Name      string    `json:"name"`
	Files     []File    `json:"files"`
	CreatedAt time.Time `json:"created_at"`
}

type File struct {
	FileName  string          `json:"file_name"`
	DirectURL string          `json:"direct_url"`
	FileSize  int64           `json:"file_size"`
	Crc32     uint32          `json:"crc32,omitempty"`
	Enc       bool            `json:"enc,omitempty"`
	MediaInfo *mediainfo.Info `json:"media_info,omitempty"`
}

func Path(infoHash string) string {
	return infoHash + "/" + FileName
}

func Key(infoHash string, idx int, name string) string {
	return fmt.Sprintf("%s/file_%d/%s", infoHash, idx, strings.ReplaceAll(name, " ", "_"))
}

func (m Manifest) Marshal() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

func Parse(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func Meta(data []byte) (name string, size int64, files []jobs.File) {
	m, err := Parse(data)
	if err != nil || len(m.Files) == 0 {
		return "", 0, nil
	}
	files = make([]jobs.File, len(m.Files))
	for i, f := range m.Files {
		files[i] = jobs.File{Index: i, Name: f.FileName, Size: f.FileSize, Key: f.DirectURL, Enc: f.Enc}
		size += f.FileSize
	}
	return m.Name, size, files
}

func ResolveKey(infoHash string, index int, storedKey, name string) string {
	if storedKey != "" {
		return storedKey
	}
	return Key(infoHash, index, name)
}

func StreamQuery(infoHash, key string, enc bool) string {
	var s string
	if strings.HasPrefix(key, "blobs/") && len(infoHash) == 40 {
		s += "&ih=" + infoHash
	}
	if enc {
		s += "&enc=1"
	}
	return s
}
