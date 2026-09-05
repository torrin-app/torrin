package addon

import (
	"net/http"
	"net/url"

	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
)

func (s *Server) entries(r *http.Request, infoHash, userID, node string, byos bool, files []jobs.File, releaseNames ...string) []map[string]any {
	out := make([]map[string]any, len(files))
	for i, file := range files {
		key := manifest.ResolveKey(infoHash, file.Index, file.Key, file.Name)
		if byos {
			key = manifest.Key(infoHash, file.Index, file.Name)
		}
		streamURL := s.streamURL(r, infoHash, key, userID, node, byos, file.Enc)
		if byos {
			streamURL += "&bk=" + url.QueryEscape(manifest.Key(infoHash, file.Index, file.Name))
		}
		out[i] = entry(file.Name, streamURL, infoHash, file.Size)
		if len(releaseNames) > 0 && releaseNames[0] != "" && releaseNames[0] != file.Name {
			out[i]["description"] = out[i]["description"].(string) + "\nRelease: " + releaseNames[0]
		}
		storageLabel := "Storage"
		if byos {
			storageLabel = "Storage"
		} else if _, _, _, direct := cairn.ParseStreamPath(key); direct {
			storageLabel = "Cairn"
		}
		out[i]["description"] = out[i]["description"].(string) + "\n" + storageLabel
	}
	return out
}
