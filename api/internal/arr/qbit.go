package arr

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/foldername"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/torrentfile"
)

const maxTorrentBytes = 10 * 1024 * 1024

func (h *Handler) qbitLogin(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	token := r.FormValue("password")
	if token == "" {
		token = r.FormValue("username")
	}
	if h.resolve(r.Context(), token) == nil {
		writePlain(w, 200, "Fails.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "SID", Value: token, Path: "/", HttpOnly: true})
	writePlain(w, 200, "Ok.")
}

func (h *Handler) qbitVersion(w http.ResponseWriter, r *http.Request) { writePlain(w, 200, "v4.6.4") }
func (h *Handler) qbitWebAPIVersion(w http.ResponseWriter, r *http.Request) {
	writePlain(w, 200, "2.9.3")
}

func (h *Handler) qbitOK(w http.ResponseWriter, r *http.Request, user *auth.User) {
	writePlain(w, 200, "Ok.")
}

func (h *Handler) qbitPreferences(w http.ResponseWriter, r *http.Request, user *auth.User) {
	writeJSON(w, 200, map[string]any{
		"save_path": h.SavePath, "temp_path_enabled": false, "queueing_enabled": false,
		"dht": true, "max_ratio_enabled": false, "max_seeding_time_enabled": false,
		"max_active_downloads": 0, "max_active_torrents": 0, "max_active_uploads": 0,
	})
}

func (h *Handler) qbitCategories(w http.ResponseWriter, r *http.Request, user *auth.User) {
	out := map[string]any{}
	for _, c := range categoryNames(h.labels.byUser(r.Context(), user.ID)) {
		if c == "*" {
			continue
		}
		out[c] = map[string]any{"name": c, "savePath": h.SavePath}
	}
	writeJSON(w, 200, out)
}

func (h *Handler) qbitAdd(w http.ResponseWriter, r *http.Request, user *auth.User) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		r.ParseForm()
	}
	plan, _ := plans.Get(user.PlanID)
	cat := r.FormValue("category")
	added, code, msg := 0, 0, ""

	for _, u := range strings.FieldsFunc(r.FormValue("urls"), func(c rune) bool { return c == '\n' || c == '\r' }) {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		_, ih, c, m := h.addTorrent(r.Context(), user, plan, u, magnet.DisplayName(u))
		if c == 0 {
			h.labels.set(r.Context(), user.ID, ih, cat)
			added++
		} else {
			code, msg = c, m
		}
	}

	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File["torrents"] {
			f, err := fh.Open()
			if err != nil {
				continue
			}
			data, _ := io.ReadAll(io.LimitReader(f, maxTorrentBytes))
			f.Close()
			meta, err := torrentfile.Parse(data)
			if err != nil || meta.InfoHash == "" {
				code, msg = 400, "invalid torrent file"
				continue
			}
			_, ih, c, m := h.addTorrent(r.Context(), user, plan, magnet.Build(meta.InfoHash, meta.Name), meta.Name)
			if c == 0 {
				h.labels.set(r.Context(), user.ID, ih, cat)
				added++
			} else {
				code, msg = c, m
			}
		}
	}

	if added == 0 && code != 0 {
		writePlain(w, code, msg)
		return
	}
	writePlain(w, 200, "Ok.")
}

func (h *Handler) qbitInfo(w http.ResponseWriter, r *http.Request, user *auth.User) {
	q := r.URL.Query()
	catFilter := q.Get("category")
	hashFilter := map[string]bool{}
	for _, hh := range strings.Split(q.Get("hashes"), "|") {
		if hh = strings.ToLower(strings.TrimSpace(hh)); hh != "" {
			hashFilter[hh] = true
		}
	}
	labels := h.labels.byUser(r.Context(), user.ID)
	list, _ := jobs.ListAll(r.Context(), h.Jobs, user.ID)
	fm := foldername.Map(list)
	out := []map[string]any{}
	for _, j := range list {
		if j.Source != jobs.SourceTorrent {
			continue
		}
		cat := labels[j.InfoHash]
		if catFilter != "" && cat != catFilter {
			continue
		}
		if len(hashFilter) > 0 && !hashFilter[strings.ToLower(j.InfoHash)] {
			continue
		}
		out = append(out, h.jobToQbitTorrent(j, cat, folderFor(fm, j)))
	}
	writeJSON(w, 200, out)
}

func (h *Handler) qbitProperties(w http.ResponseWriter, r *http.Request, user *auth.User) {
	j := h.ownedByHash(r.Context(), user, r.URL.Query().Get("hash"))
	if j == nil {
		writeJSON(w, 200, map[string]any{})
		return
	}
	writeJSON(w, 200, map[string]any{
		"save_path": h.SavePath, "comment": "", "total_size": totalBytes(j),
		"addition_date": j.CreatedAt.Unix(), "completion_date": j.UpdatedAt.Unix(),
		"created_by": "torrin", "piece_size": 0, "pieces_num": 0,
	})
}

func (h *Handler) qbitFiles(w http.ResponseWriter, r *http.Request, user *auth.User) {
	j := h.ownedByHash(r.Context(), user, r.URL.Query().Get("hash"))
	if j == nil {
		writeJSON(w, 200, []any{})
		return
	}
	writeJSON(w, 200, qbitFileList(j))
}

func (h *Handler) qbitDelete(w http.ResponseWriter, r *http.Request, user *auth.User) {
	r.ParseForm()
	for _, hh := range strings.Split(r.FormValue("hashes"), "|") {
		if hh = strings.ToLower(strings.TrimSpace(hh)); hh == "" {
			continue
		}
		j, err := h.Jobs.GetByUserInfoHash(r.Context(), user.ID, hh)
		if err != nil || j == nil {
			continue
		}
		if j.Status.Active() && h.Qbit != nil && h.Qbit.Login() == nil {
			h.Qbit.Delete(j.InfoHash)
		}
		h.labels.del(r.Context(), user.ID, j.InfoHash)
		h.Jobs.Delete(r.Context(), j.ID)
	}
	writePlain(w, 200, "Ok.")
}

func (h *Handler) ownedByHash(ctx context.Context, user *auth.User, hash string) *jobs.Job {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return nil
	}
	j, err := h.Jobs.GetByUserInfoHash(ctx, user.ID, hash)
	if err != nil {
		return nil
	}
	return j
}
