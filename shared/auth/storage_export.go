package auth

import (
	"fmt"
	"sort"
	"strings"
)

func isSecretParam(k string) bool {
	switch k {
	case "pass", "password", "access_token", "token":
		return true
	}
	return false
}

func (c *StorageCreds) RcloneConfig(obscure func(string) string) string {
	specs := c.remoteSpecs()
	var b strings.Builder
	b.WriteString("# Torrin storage config. Keep this private: it holds your access keys")
	if c.Encrypted {
		b.WriteString(" and encryption password")
	}
	b.WriteString(".\n")
	if c.Encrypted {
		b.WriteString("# If a password field below is your raw passphrase, run: rclone obscure '<value>'\n")
	}
	b.WriteString("\n")

	srcNames := make([]string, len(specs))
	for i := range specs {
		srcNames[i] = fmt.Sprintf("torrin-src%d", i)
	}

	root := srcNames[0] + ":" + specs[0].BasePath
	if len(specs) > 1 {
		root = "torrin-pool:"
	}
	if c.Encrypted {
		fmt.Fprintf(&b, "[torrin]\ntype = crypt\nremote = %s\npassword = %s\nfilename_encryption = standard\ndirectory_name_encryption = true\n\n", root, obscure(c.CryptPass))
	}
	if len(specs) > 1 {
		ups := make([]string, len(specs))
		for i, s := range specs {
			ups[i] = srcNames[i] + ":" + s.BasePath
		}
		policy := c.UnionPolicy
		if policy == "" {
			policy = "epmfs"
		}
		fmt.Fprintf(&b, "[torrin-pool]\ntype = union\nupstreams = %s\ncreate_policy = %s\n\n", strings.Join(ups, " "), policy)
	}
	for i, s := range specs {
		fmt.Fprintf(&b, "[%s]\ntype = %s\n", srcNames[i], s.Backend)
		keys := make([]string, 0, len(s.Params))
		for k := range s.Params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := s.Params[k]
			if isSecretParam(k) {
				v = obscure(v)
			}
			fmt.Fprintf(&b, "%s = %s\n", k, v)
		}
		b.WriteString("\n")
	}
	return b.String()
}
