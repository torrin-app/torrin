package rclonerc

import (
	"context"
	"fmt"
)

func (c *Client) CopyFileAsync(ctx context.Context, srcFs, srcRemote, dstFs, dstRemote, group string) (int64, error) {
	out, err := c.call(ctx, "operations/copyfile", map[string]any{
		"srcFs": srcFs, "srcRemote": srcRemote,
		"dstFs": dstFs, "dstRemote": dstRemote,
		"_async": true, "_group": group,
	})
	if err != nil {
		return 0, err
	}
	id, ok := out["jobid"].(float64)
	if !ok {
		return 0, fmt.Errorf("copyfile: no jobid in response")
	}
	return int64(id), nil
}

type JobState struct {
	Finished bool
	Success  bool
	Error    string
}

func (c *Client) JobStatus(ctx context.Context, jobID int64) (JobState, error) {
	out, err := c.call(ctx, "job/status", map[string]any{"jobid": jobID})
	if err != nil {
		return JobState{}, err
	}
	st := JobState{}
	st.Finished, _ = out["finished"].(bool)
	st.Success, _ = out["success"].(bool)
	st.Error, _ = out["error"].(string)
	return st, nil
}

func (c *Client) JobStop(ctx context.Context, jobID int64) error {
	_, err := c.call(ctx, "job/stop", map[string]any{"jobid": jobID})
	return err
}

func (c *Client) GroupBytes(ctx context.Context, group string) int64 {
	out, err := c.call(ctx, "core/stats", map[string]any{"group": group})
	if err != nil {
		return 0
	}
	b, _ := out["bytes"].(float64)
	return int64(b)
}
