package arr

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/foldername"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/safeurl"
)

const (
	sabVersion  = "3.7.2"
	maxNzbBytes = 20 * 1024 * 1024
)

func (h *Handler) sab(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode == "version" {
		writeJSON(w, 200, map[string]any{"version": sabVersion})
		return
	}
	user := h.resolve(r.Context(), r.URL.Query().Get("apikey"))
	if user == nil {
		writeJSON(w, 200, map[string]any{"status": false, "error": "API Key Incorrect"})
		return
	}
	switch mode {
	case "get_config":
		h.sabConfig(w, r, user)
	case "get_cats":
		writeJSON(w, 200, map[string]any{"categories": categoryNames(h.labels.byUser(r.Context(), user.ID))})
	case "fullstatus":
		writeJSON(w, 200, map[string]any{"status": map[string]any{"version": sabVersion}})
	case "addfile", "addurl":
		h.sabAdd(w, r, user, mode)
	case "queue":
		h.sabQueue(w, r, user)
	case "history":
		h.sabHistory(w, r, user)
	default:
		writeJSON(w, 200, map[string]any{"status": true})
	}
}

func (h *Handler) sabConfig(w http.ResponseWriter, r *http.Request, user *auth.User) {
	cats := []map[string]any{}
	for _, c := range categoryNames(h.labels.byUser(r.Context(), user.ID)) {
		cats = append(cats, map[string]any{"name": c, "dir": ""})
	}
	writeJSON(w, 200, map[string]any{"config": map[string]any{
		"misc": map[string]any{
			"complete_dir": h.SavePath, "pre_check": 0, "sfv_check": 0,
			"enable_tv_sorting": 0, "enable_movie_sorting": 0, "enable_date_sorting": 0,
		},
		"categories": cats,
	}})
}

func (h *Handler) sabAdd(w http.ResponseWriter, r *http.Request, user *auth.User, mode string) {
	if !h.usenetOK(r.Context(), user) {
		writeJSON(w, 200, map[string]any{"status": false, "error": "usenet is not enabled on your plan"})
		return
	}
	plan, _ := plans.Get(user.PlanID)
	q := r.URL.Query()
	cat := q.Get("cat")
	body, name, ok := h.sabReadNzb(w, r, mode)
	if !ok {
		return
	}
	if v := q.Get("nzbname"); v != "" {
		name = strings.TrimSuffix(v, ".nzb")
	}
	id, ih, code, msg := h.addUsenet(r.Context(), user, plan, body, name)
	if code != 0 {
		writeJSON(w, 200, map[string]any{"status": false, "error": msg})
		return
	}
	h.labels.set(r.Context(), user.ID, ih, cat)
	writeJSON(w, 200, map[string]any{"status": true, "nzo_ids": []string{id}})
}

func (h *Handler) sabReadNzb(w http.ResponseWriter, r *http.Request, mode string) ([]byte, string, bool) {
	if mode == "addfile" {
		f, hdr, err := r.FormFile("name")
		if err != nil {
			f, hdr, err = r.FormFile("nzbfile")
		}
		if err != nil {
			writeJSON(w, 200, map[string]any{"status": false, "error": "no nzb file"})
			return nil, "", false
		}
		defer f.Close()
		data, _ := io.ReadAll(io.LimitReader(f, maxNzbBytes))
		return data, strings.TrimSuffix(hdr.Filename, ".nzb"), true
	}
	link := r.URL.Query().Get("name")
	if link == "" {
		link = r.FormValue("name")
	}
	if link == "" {
		writeJSON(w, 200, map[string]any{"status": false, "error": "no nzb url"})
		return nil, "", false
	}
	resp, err := safeurl.Client(30 * time.Second).Get(link)
	if err != nil || resp.StatusCode >= 400 {
		if resp != nil {
			resp.Body.Close()
		}
		writeJSON(w, 200, map[string]any{"status": false, "error": "could not fetch nzb"})
		return nil, "", false
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxNzbBytes))
	return data, "", true
}

func (h *Handler) sabQueue(w http.ResponseWriter, r *http.Request, user *auth.User) {
	if r.URL.Query().Get("name") == "delete" {
		h.sabDelete(r.Context(), user, r.URL.Query().Get("value"))
		writeJSON(w, 200, map[string]any{"status": true})
		return
	}
	labels := h.labels.byUser(r.Context(), user.ID)
	list, _ := jobs.ListAll(r.Context(), h.Jobs, user.ID)
	slots := []map[string]any{}
	for _, j := range list {
		if j.Source != jobs.SourceUsenet || !j.Status.Active() {
			continue
		}
		slots = append(slots, sabQueueSlot(j, labels[j.InfoHash]))
	}
	writeJSON(w, 200, map[string]any{"queue": map[string]any{
		"paused": false, "slots": slots, "noofslots": len(slots),
		"speedlimit": "100", "speedlimit_abs": "", "diskspacetotal1": "1000", "diskspace1": "1000",
	}})
}

func (h *Handler) sabHistory(w http.ResponseWriter, r *http.Request, user *auth.User) {
	if r.URL.Query().Get("name") == "delete" {
		h.sabDelete(r.Context(), user, r.URL.Query().Get("value"))
		writeJSON(w, 200, map[string]any{"status": true})
		return
	}
	labels := h.labels.byUser(r.Context(), user.ID)
	list, _ := jobs.ListAll(r.Context(), h.Jobs, user.ID)
	fm := foldername.Map(list)
	slots := []map[string]any{}
	for _, j := range list {
		if j.Source != jobs.SourceUsenet {
			continue
		}
		if j.Status == jobs.StatusComplete || j.Status == jobs.StatusFailed {
			slots = append(slots, sabHistorySlot(j, labels[j.InfoHash], h.SavePath, folderFor(fm, j)))
		}
	}
	writeJSON(w, 200, map[string]any{"history": map[string]any{"slots": slots, "noofslots": len(slots)}})
}

func (h *Handler) sabDelete(ctx context.Context, user *auth.User, id string) {
	if id == "" || id == "all" {
		return
	}
	j, err := h.Jobs.Get(ctx, id)
	if err != nil || j.UserID != user.ID {
		return
	}
	h.labels.del(ctx, user.ID, j.InfoHash)
	h.Jobs.Delete(ctx, j.ID)
}
