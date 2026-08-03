package indexer

import (
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/safeurl"
	"github.com/torrin-app/torrin/shared/useragent"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type Result struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Size      int64     `json:"size"`
	PubDate   time.Time `json:"pub_date"`
	NZBURL    string    `json:"nzb_url"`
	Category  string    `json:"category"`
	IMDBID    string    `json:"imdb_id,omitempty"`
	IMDBTitle string    `json:"imdb_title,omitempty"`
	IMDBYear  int       `json:"imdb_year,omitempty"`
	Grabs     int       `json:"grabs"`
}

func NewClient(baseURL, apiKey string) *Client {
	return newClient(baseURL, apiKey, false)
}

func NewTestClient(baseURL, apiKey string) *Client {
	return newClient(baseURL, apiKey, true)
}

func newClient(baseURL, apiKey string, allowLocal bool) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = safeurl.Dialer(allowLocal)
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second, Transport: transport},
	}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("only http/https allowed")
	}
	ips, err := net.LookupHost(u.Hostname())
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}
	for _, ipStr := range ips {
		if ip := net.ParseIP(ipStr); ip != nil && safeurl.Blocked(ip) {
			return fmt.Errorf("private/internal addresses not allowed")
		}
	}
	return nil
}

func (c *Client) SearchMovie(imdbID string) ([]Result, error) {
	return c.search(url.Values{
		"t": {"movie"}, "imdbid": {strings.TrimPrefix(imdbID, "tt")},
		"cat": {"2000,2040,2045,2050"}, "extended": {"1"}, "limit": {"50"}, "apikey": {c.apiKey},
	})
}

func (c *Client) SearchTV(imdbID string, season, episode int) ([]Result, error) {
	return c.search(url.Values{
		"t": {"tvsearch"}, "imdbid": {strings.TrimPrefix(imdbID, "tt")},
		"season": {strconv.Itoa(season)}, "ep": {strconv.Itoa(episode)},
		"cat": {"5000,5040,5045"}, "extended": {"1"}, "limit": {"50"}, "apikey": {c.apiKey},
	})
}

func (c *Client) SearchQuery(query, categories string) ([]Result, error) {
	if categories == "" {
		categories = "2000,5000"
	}
	return c.search(url.Values{
		"t": {"search"}, "q": {query}, "cat": {categories},
		"extended": {"1"}, "limit": {"50"}, "apikey": {c.apiKey},
	})
}

func (c *Client) DownloadNZB(result *Result) ([]byte, error) {
	nzbURL := result.NZBURL
	if nzbURL == "" {
		nzbURL = fmt.Sprintf("%s/api?t=get&id=%s&apikey=%s", c.baseURL, result.ID, c.apiKey)
	}
	resp, err := c.get(nzbURL)
	if err != nil {
		return nil, fmt.Errorf("download nzb: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download nzb: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) get(rawURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", useragent.Default)
	return c.httpClient.Do(req)
}

func (c *Client) search(params url.Values) ([]Result, error) {
	resp, err := c.get(fmt.Sprintf("%s/api?%s", c.baseURL, params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("indexer request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("indexer error (%d): %s", resp.StatusCode, body)
	}

	var apiErr apiError
	if xml.Unmarshal(body, &apiErr) == nil && apiErr.Code != 0 {
		return nil, fmt.Errorf("indexer api error %d: %s", apiErr.Code, apiErr.Description)
	}
	var rss rssResponse
	if err := xml.Unmarshal(body, &rss); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var results []Result
	for _, item := range rss.Channel.Items {
		r := Result{Title: item.Title, NZBURL: item.Link, PubDate: parseDate(item.PubDate)}
		if item.Enclosure.URL != "" {
			r.NZBURL = item.Enclosure.URL
			r.Size = item.Enclosure.Length
		}
		for _, attr := range item.Attrs {
			switch attr.Name {
			case "guid":
				r.ID = attr.Value
			case "size":
				if s, e := strconv.ParseInt(attr.Value, 10, 64); e == nil {
					r.Size = s
				}
			case "category":
				r.Category = attr.Value
			case "imdb":
				r.IMDBID = attr.Value
			case "imdbtitle":
				r.IMDBTitle = attr.Value
			case "imdbyear":
				if y, e := strconv.Atoi(attr.Value); e == nil {
					r.IMDBYear = y
				}
			case "grabs":
				if g, e := strconv.Atoi(attr.Value); e == nil {
					r.Grabs = g
				}
			}
		}
		if r.ID == "" {
			r.ID = extractGUID(item.GUID.Value)
		}
		results = append(results, r)
	}
	return results, nil
}

func parseDate(s string) time.Time {
	for _, f := range []string{time.RFC1123Z, time.RFC1123, "Mon, 02 Jan 2006 15:04:05 -0700", "Mon, 02 Jan 2006 15:04:05 MST"} {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func extractGUID(link string) string {
	parts := strings.Split(strings.TrimRight(link, "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return link
}

type apiError struct {
	XMLName     xml.Name `xml:"error"`
	Code        int      `xml:"code,attr"`
	Description string   `xml:"description,attr"`
}

type rssResponse struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}
type rssChannel struct {
	Items []rssItem `xml:"item"`
}
type rssItem struct {
	Title     string       `xml:"title"`
	Link      string       `xml:"link"`
	GUID      rssGUID      `xml:"guid"`
	PubDate   string       `xml:"pubDate"`
	Enclosure rssEnclosure `xml:"enclosure"`
	Attrs     []nzbAttr    `xml:"http://www.newznab.com/DTD/2010/feeds/attributes/ attr"`
}
type rssGUID struct {
	Value string `xml:",chardata"`
}
type rssEnclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}
type nzbAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}
