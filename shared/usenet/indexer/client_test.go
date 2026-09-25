package indexer

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/torrin-app/torrin/shared/useragent"
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

func TestUserAgentPerOp(t *testing.T) {
	var searchUA, grabUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "" {
			searchUA = r.UserAgent()
			w.Write([]byte(sampleXML))
			return
		}
		grabUA = r.UserAgent()
		w.Write([]byte("<nzb/>"))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "k", "MySearch/1", "MyGrab/2", true)
	c.SearchQuery("x", "2000", 0, 5)
	c.DownloadNZB(&Result{NZBURL: srv.URL + "/nzb"})
	if searchUA != "MySearch/1" {
		t.Errorf("search UA = %q, want MySearch/1", searchUA)
	}
	if grabUA != "MyGrab/2" {
		t.Errorf("grab UA = %q, want MyGrab/2", grabUA)
	}

	d := newClient(srv.URL, "k", "", "", true)
	d.SearchQuery("x", "2000", 0, 5)
	d.DownloadNZB(&Result{NZBURL: srv.URL + "/nzb"})
	if searchUA != useragent.Indexer {
		t.Errorf("default search UA = %q, want %q", searchUA, useragent.Indexer)
	}
	if grabUA != useragent.IndexerGrab {
		t.Errorf("default grab UA = %q, want %q", grabUA, useragent.IndexerGrab)
	}
}

func TestGrabUAByCategory(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.UserAgent()
		w.Write([]byte("<nzb/>"))
	}))
	defer srv.Close()

	grab := func(c *Client, cat string) string {
		got = ""
		c.DownloadNZB(&Result{NZBURL: srv.URL + "/nzb", Category: cat})
		return got
	}

	def := newClient(srv.URL, "k", "", "", true)
	if ua := grab(def, "5040"); ua != useragent.Sonarr {
		t.Errorf("tv grab UA = %q, want %q", ua, useragent.Sonarr)
	}
	if ua := grab(def, "2040"); ua != useragent.Radarr {
		t.Errorf("movie grab UA = %q, want %q", ua, useragent.Radarr)
	}
	if ua := grab(def, ""); ua != useragent.IndexerGrab {
		t.Errorf("uncategorized grab UA = %q, want %q", ua, useragent.IndexerGrab)
	}

	custom := newClient(srv.URL, "k", "", "MyGrab/9", true)
	if ua := grab(custom, "5040"); ua != "MyGrab/9" {
		t.Errorf("explicit grab UA overridden by category: %q", ua)
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
