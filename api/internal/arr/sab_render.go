package arr

import (
	"fmt"
	"strconv"

	"github.com/torrin-app/torrin/shared/jobs"
)

func sabQueueSlot(j *jobs.Job, category string) map[string]any {
	total := j.FileSize
	if total <= 0 {
		total = totalBytes(j)
	}
	pct := int(j.Progress)
	if pct < 0 || pct > 100 {
		pct = 0
	}
	left := total
	if total > 0 && pct > 0 {
		left = total - total*int64(pct)/100
	}
	return map[string]any{
		"nzo_id": j.ID, "filename": qbitName(j), "cat": catOrDefault(category),
		"status": sabQueueStatus(j.Status), "priority": "Normal", "percentage": strconv.Itoa(pct),
		"mb": mbStr(total), "mbleft": mbStr(left), "size": sizeStr(total), "sizeleft": sizeStr(left),
		"timeleft": "0:00:00",
	}
}

func sabHistorySlot(j *jobs.Job, category, savePath, folder string) map[string]any {
	status, fail := "Completed", ""
	if j.Status == jobs.StatusFailed {
		status, fail = "Failed", j.Error
	}
	return map[string]any{
		"nzo_id": j.ID, "name": qbitName(j), "nzb_name": qbitName(j) + ".nzb",
		"category": catOrDefault(category), "status": status, "fail_message": fail,
		"storage": contentPath(savePath, folder), "path": contentPath(savePath, folder),
		"bytes": totalBytes(j), "download_time": 0, "postproc_time": 0, "completed": j.UpdatedAt.Unix(),
	}
}

func sabQueueStatus(s jobs.Status) string {
	switch s {
	case jobs.StatusQueued, jobs.StatusPending:
		return "Queued"
	default:
		return "Downloading"
	}
}

func mbStr(bytes int64) string { return fmt.Sprintf("%.2f", float64(bytes)/(1024*1024)) }
func sizeStr(bytes int64) string {
	return fmt.Sprintf("%.2f MB", float64(bytes)/(1024*1024))
}
