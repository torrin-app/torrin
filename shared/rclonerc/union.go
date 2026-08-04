package rclonerc

import (
	"context"
	"fmt"
	"strings"
)

type RemoteSpec struct {
	Backend  string
	Params   map[string]string
	Obscure  bool
	BasePath string
}

func (c *Client) EnsureUserUnion(ctx context.Context, userID string, srcs []RemoteSpec, policy, cryptPassword string) (string, error) {
	if len(srcs) == 0 {
		return "", fmt.Errorf("no providers")
	}
	remote := UserRemoteName(userID)
	upstreams := make([]string, 0, len(srcs))
	for i, s := range srcs {
		name := fmt.Sprintf("%s_src%d", remote, i)
		if err := c.CreateRemote(ctx, name, s.Backend, s.Params, s.Obscure); err != nil {
			return "", err
		}
		upstreams = append(upstreams, name+":"+s.BasePath)
	}
	if policy == "" {
		policy = "epmfs"
	}
	unionName := remote + "_union"
	if err := c.CreateRemote(ctx, unionName, "union", map[string]string{
		"upstreams":     strings.Join(upstreams, " "),
		"create_policy": policy,
	}, false); err != nil {
		return "", err
	}
	if cryptPassword == "" {
		return unionName, nil
	}
	crypt := map[string]string{
		"remote":                    unionName + ":",
		"password":                  cryptPassword,
		"filename_encryption":       "standard",
		"directory_name_encryption": "true",
	}
	if err := c.CreateRemote(ctx, remote, "crypt", crypt, true); err != nil {
		return "", err
	}
	return remote, nil
}

func (c *Client) Obscure(ctx context.Context, plain string) (string, bool) {
	out, err := c.call(ctx, "core/command", map[string]any{
		"command":    "obscure",
		"arg":        []string{plain},
		"returnType": "COMBINED_OUTPUT",
	})
	if err != nil || out == nil {
		return plain, false
	}
	if s, ok := out["result"].(string); ok {
		if s = strings.TrimSpace(s); s != "" {
			return s, true
		}
	}
	return plain, false
}
