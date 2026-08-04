package handlers

import "testing"

func TestBuildProvider(t *testing.T) {
	s3, err := buildProvider(providerReq{Provider: "s3", Endpoint: "s3.example.com", Bucket: "b", AccessKey: "AK", SecretKey: "SK"})
	if err != nil || s3.Backend != "s3" || s3.Bucket != "b" {
		t.Fatalf("s3: %+v %v", s3, err)
	}
	if s3.Params["endpoint"] != "https://s3.example.com" {
		t.Errorf("s3 endpoint should be https-normalized: %q", s3.Params["endpoint"])
	}

	mega, err := buildProvider(providerReq{Provider: "mega", User: "u", Pass: "p"})
	if err != nil || mega.Backend != "mega" || mega.Params["user"] != "u" || mega.Params["pass"] != "p" {
		t.Fatalf("mega: %+v %v", mega, err)
	}

	gof, err := buildProvider(providerReq{Provider: "gofile", Token: "tok"})
	if err != nil || gof.Backend != "gofile" || gof.Params["access_token"] != "tok" {
		t.Fatalf("gofile: %+v %v", gof, err)
	}

	cfg, err := buildProvider(providerReq{Config: "[d]\ntype = drive\ntoken = abc"})
	if err != nil || cfg.Backend != "drive" || cfg.Params["token"] != "abc" {
		t.Fatalf("config: %+v %v", cfg, err)
	}

	if _, err := buildProvider(providerReq{Provider: "s3", Bucket: "b"}); err == nil {
		t.Error("s3 missing keys should error")
	}
}
