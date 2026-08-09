package indexer

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleXML = `<?xml version="1.0"?>
<rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
 <channel><item>
   <title>Some.Movie.2021.1080p</title>
   <link>https://idx.example/details/abc123</link>
   <enclosure url="https://idx.example/getnzb/abc123.nzb" length="123456789" type="application/x-nzb"/>
   <newznab:attr name="size" value="123456789"/>
   <newznab:attr name="imdb" value="0816692"/>
   <newznab:attr name="grabs" value="42"/>
 </item></channel>
</rss>`

func TestSearchParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "movie" {
			t.Errorf("t=%q", r.URL.Query().Get("t"))
		}
		w.Write([]byte(sampleXML))
	}))
	defer srv.Close()

	res, err := NewTestClient(srv.URL, "k").SearchMovie("tt0816692", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("want 1 result, got %d", len(res))
	}
	r := res[0]
	if r.Title != "Some.Movie.2021.1080p" || r.Size != 123456789 || r.IMDBID != "0816692" || r.Grabs != 42 {
		t.Errorf("parsed wrong: %+v", r)
	}
	if r.NZBURL != "https://idx.example/getnzb/abc123.nzb" {
		t.Errorf("nzb url = %q", r.NZBURL)
	}
}

func TestValidateURL(t *testing.T) {
	if ValidateURL("http://127.0.0.1/api") == nil {
		t.Error("loopback should be rejected")
	}
	if ValidateURL("ftp://example.com") == nil {
		t.Error("non-http scheme should be rejected")
	}
}
