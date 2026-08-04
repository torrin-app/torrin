package auth

import (
	"strings"
	"testing"
)

func ident(s string) string { return s }

func TestRcloneConfigSingleEncrypted(t *testing.T) {
	c := &StorageCreds{
		Backend: "s3", Bucket: "mybucket",
		ConfigJSON: `{"endpoint":"https://e","access_key_id":"AK"}`,
		Encrypted:  true, CryptPass: "secret",
	}
	out := c.RcloneConfig(func(s string) string { return "OBS(" + s + ")" })
	if !strings.Contains(out, "type = crypt") || !strings.Contains(out, "password = OBS(secret)") {
		t.Fatalf("crypt section missing/unobscured:\n%s", out)
	}
	if !strings.Contains(out, "remote = torrin-src0:mybucket") {
		t.Fatalf("single-provider crypt should point straight at src0:\n%s", out)
	}
	if strings.Contains(out, "type = union") {
		t.Fatalf("no union expected for a single provider:\n%s", out)
	}
}

func TestRcloneConfigUnion(t *testing.T) {
	c := &StorageCreds{
		Backend: "s3", Bucket: "b0", ConfigJSON: `{"endpoint":"https://e0"}`,
		Encrypted: true, CryptPass: "pw", UnionPolicy: "all",
		Providers: `[{"backend":"s3","bucket":"b1","params":{"endpoint":"https://e1"}}]`,
	}
	if !c.HasUnion() {
		t.Fatal("HasUnion should be true with an extra provider")
	}
	out := c.RcloneConfig(ident)
	if !strings.Contains(out, "type = union") || !strings.Contains(out, "create_policy = all") {
		t.Fatalf("union section missing:\n%s", out)
	}
	if !strings.Contains(out, "torrin-src0:b0") || !strings.Contains(out, "torrin-src1:b1") {
		t.Fatalf("both upstreams expected:\n%s", out)
	}
	if !strings.Contains(out, "remote = torrin-pool:") {
		t.Fatalf("crypt should wrap the pool:\n%s", out)
	}
}

func TestRcloneConfigObscuresBackendSecret(t *testing.T) {
	c := &StorageCreds{
		Backend: "mega", ConfigJSON: `{"user":"u","pass":"p"}`,
		Encrypted: false,
	}
	out := c.RcloneConfig(func(s string) string { return "X" + s })
	if !strings.Contains(out, "pass = Xp") {
		t.Fatalf("backend password should be obscured:\n%s", out)
	}
	if !strings.Contains(out, "user = u") {
		t.Fatalf("non-secret field should stay plain:\n%s", out)
	}
}
