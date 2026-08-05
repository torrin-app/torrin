package nzburl

import "testing"

func TestHashMatchesAIOStreams(t *testing.T) {
	cases := map[string]string{
		"https://indexer.example.com/api?t=get&id=abc123DEF&apikey=SECRET":                                           "6b47fafb137e996e9546a04c11c498d3",
		"https://indexer.example.com/api?apikey=SECRET&t=get&id=abc-123.xyz":                                         "445fddddc4cdcd07fb9b9418fbe46286",
		"https://idx.test/api?t=g&id=zz99&extra=1&foo=bar":                                                           "a927eb3f1102be893aad57cef2145dc8",
		"https://prowlarr.local:9696/1/download?apikey=KEY&link=https%3A%2F%2Fusenet.io%2Fnzb%3Fg%3D5%26h%3Da+b*c~d": "b6fff50ad8ee4c125ff683818162553b",
		"https://prowlarr.local/download?link=aHR0cHM6Ly94Lnk%3D":                                                    "1fc97356c49b2b762a05c0c611912840",
		"https://nzb.host/getnzb/somefile?id=GUID-123&r=apikey":                                                      "15595a7e271b40012aac3e1d50c3ba1a",
		"https://nzb.host/getnzb?id=plain":                                                                           "f647f8d81ffc4ea3eaeb7ebc30b19f44",
		"https://nowhere.test/api?t=search&id=x":                                                                     "19c9cfdfd95408dcf44bdde5e6f97139",
		"https://random.site/foo/bar?a=1&b=2#frag":                                                                   "8a429ad66b0335a1bc450d6e0864b8a3",
		"not a url at all": "f2f8467a4306ce6bd4c1066600ce83cb",
	}
	for in, want := range cases {
		if got := Hash(in); got != want {
			t.Errorf("Hash(%q) = %s, want %s", in, got, want)
		}
	}
}
