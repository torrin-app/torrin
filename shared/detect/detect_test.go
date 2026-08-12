package detect

import "testing"

func TestDetect(t *testing.T) {
	hash := "c12fe1c06bba254a9dc9f519b335aa7c1367a88a"
	torrent := []byte("d8:announce20:http://tracker/annce4:infod6:lengthi1e4:name5:movieee")
	nzb := []byte(`<?xml version="1.0"?>` + "\n" + `<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><file></file></nzb>`)

	cases := []struct {
		name     string
		data     []byte
		filename string
		want     Kind
	}{
		{"magnet uri", []byte("magnet:?xt=urn:btih:" + hash), "", Magnet},
		{"magnet uppercase scheme", []byte("MAGNET:?xt=urn:btih:" + hash), "", Magnet},
		{"bare infohash", []byte(hash), "", InfoHash},
		{"infohash with whitespace", []byte("  " + hash + "\n"), "", InfoHash},
		{"http url", []byte("https://youtube.com/watch?v=abc"), "", URL},
		{"torrent by content", torrent, "", Torrent},
		{"torrent by extension", []byte("d...garbage"), "file.torrent", Torrent},
		{"nzb by content", nzb, "", NZB},
		{"nzb by extension", []byte("<?xml?><other/>"), "release.NZB", NZB},
		{"nonsense", []byte("just some words"), "", Unknown},
		{"empty", []byte(""), "", Unknown},
	}
	for _, c := range cases {
		if got := Detect(c.data, c.filename); got != c.want {
			t.Errorf("%s: Detect() = %d, want %d", c.name, got, c.want)
		}
	}
}
