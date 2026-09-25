package arr

import "github.com/torrin-app/torrin/shared/jobs"

func (h *Handler) jobToQbitTorrent(j *jobs.Job, category, folder string) map[string]any {
	size := totalBytes(j)
	if size == 0 {
		size = j.FileSize
	}
	done := j.Status == jobs.StatusComplete || j.Status == jobs.StatusSeeding
	progress := j.Progress / 100
	if progress < 0 || progress > 1 {
		progress = 0
	}
	amountLeft, completion, eta, speed := size, int64(0), int64(8640000), j.Speed
	if done {
		progress, amountLeft, completion, eta, speed = 1, 0, j.UpdatedAt.Unix(), 0, 0
	}
	return map[string]any{
		"hash": j.InfoHash, "name": qbitName(j), "size": size, "total_size": size,
		"progress": progress, "dlspeed": speed, "upspeed": 0, "eta": eta,
		"state": qbitState(j.Status), "category": category, "tags": "",
		"save_path": h.SavePath, "content_path": contentPath(h.SavePath, folder),
		"added_on": j.CreatedAt.Unix(), "completion_on": completion,
		"amount_left": amountLeft, "completed": size - amountLeft,
		"ratio": 0, "seeding_time": 0, "num_seeds": 0, "num_leechs": 0, "priority": 0,
	}
}

func qbitName(j *jobs.Job) string {
	if j.Name != "" {
		return j.Name
	}
	return j.InfoHash
}

func qbitState(s jobs.Status) string {
	switch s {
	case jobs.StatusComplete, jobs.StatusSeeding:
		return "pausedUP"
	case jobs.StatusDownloading, jobs.StatusProcessing:
		return "downloading"
	case jobs.StatusPublishing:
		return "checkingUP"
	case jobs.StatusQueued:
		return "queuedDL"
	case jobs.StatusFailed:
		return "error"
	case jobs.StatusEvicted:
		return "missingFiles"
	default:
		return "metaDL"
	}
}

func qbitFileList(j *jobs.Job) []map[string]any {
	done := j.Status == jobs.StatusComplete || j.Status == jobs.StatusSeeding
	out := []map[string]any{}
	for i, f := range j.Files {
		p := float64(0)
		if done {
			p = 1
		}
		out = append(out, map[string]any{
			"index": i, "name": f.Name, "size": f.Size, "progress": p,
			"priority": 1, "is_seed": false, "availability": 1,
		})
	}
	return out
}
