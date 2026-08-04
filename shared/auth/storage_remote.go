package auth

import (
	"context"
	"encoding/json"

	"github.com/torrin-app/torrin/shared/rclonerc"
)

type StorageProvider struct {
	Backend string            `json:"backend"`
	Params  map[string]string `json:"params"`
	Bucket  string            `json:"bucket,omitempty"`
}

func (c *StorageCreds) ProviderList() []StorageProvider {
	if c.Providers == "" {
		return nil
	}
	var out []StorageProvider
	if json.Unmarshal([]byte(c.Providers), &out) != nil {
		return nil
	}
	return out
}

func (c *StorageCreds) HasUnion() bool { return len(c.ProviderList()) > 0 }

func (c *StorageCreds) primarySpec() rclonerc.RemoteSpec {
	var params map[string]string
	json.Unmarshal([]byte(c.ConfigJSON), &params)
	return rclonerc.RemoteSpec{Backend: c.Backend, Params: params, Obscure: true, BasePath: c.Bucket}
}

func (c *StorageCreds) remoteSpecs() []rclonerc.RemoteSpec {
	specs := []rclonerc.RemoteSpec{c.primarySpec()}
	for _, p := range c.ProviderList() {
		specs = append(specs, rclonerc.RemoteSpec{Backend: p.Backend, Params: p.Params, Obscure: true, BasePath: p.Bucket})
	}
	return specs
}

func EnsureRemote(ctx context.Context, rc *rclonerc.Client, userID string, c *StorageCreds) (string, error) {
	if !c.HasUnion() {
		var params map[string]string
		json.Unmarshal([]byte(c.ConfigJSON), &params)
		return rc.EnsureUserRemote(ctx, userID, c.Backend, params, true, c.CryptPass, c.Bucket)
	}
	return rc.EnsureUserUnion(ctx, userID, c.remoteSpecs(), c.UnionPolicy, c.CryptPass)
}
