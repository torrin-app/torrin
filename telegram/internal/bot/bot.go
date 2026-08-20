package bot

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/celestix/gotgproto"
	"github.com/celestix/gotgproto/dispatcher/handlers"
	"github.com/celestix/gotgproto/dispatcher/handlers/filters"
	"github.com/celestix/gotgproto/ext"
	"github.com/celestix/gotgproto/functions"
	"github.com/celestix/gotgproto/sessionMaker"

	"github.com/torrin-app/torrin/shared/blobstore"
	"github.com/torrin-app/torrin/shared/cinemeta"
	"github.com/torrin-app/torrin/shared/cluster"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/safety"
	"github.com/torrin-app/torrin/shared/sources"
)

type Publisher interface {
	Publish(subject string, v any) error
}

type Bot struct {
	AppID    int
	APIHash  string
	BotToken string
	TmpDir   string
	Node     string

	Store    sources.Store
	Overflow map[string]sources.Store
	Blobs    blobstore.Index
	Cipher   *crypto.Stream
	Sizer    cluster.Sizer
	Repo     jobs.Repository
	Bus      Publisher

	Resolve func(tgUserID int64) (userID string, ok bool)
	Link    func(code string, tgUserID int64) (userID string, ok bool)
	Plan    func(userID string) (maxBytes int64, maxConcurrent int)
	Paid    func(userID string) bool
	Ban     func(userID, reason string)

	dedup *dedupe
	cm    *cinemeta.Client
}

type dkey struct {
	user int64
	msg  int
}

type dedupe struct {
	mu   sync.Mutex
	seen map[dkey]time.Time
	ttl  time.Duration
}

func newDedupe(ttl time.Duration) *dedupe {
	return &dedupe{seen: map[dkey]time.Time{}, ttl: ttl}
}

func (d *dedupe) firstSight(user int64, msg int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for k, t := range d.seen {
		if now.Sub(t) > d.ttl {
			delete(d.seen, k)
		}
	}
	key := dkey{user, msg}
	if _, ok := d.seen[key]; ok {
		return false
	}
	d.seen[key] = now
	return true
}

func (b *Bot) Run(ctx context.Context) error {
	if b.TmpDir == "" {
		b.TmpDir = os.TempDir()
	}
	if err := os.MkdirAll(b.TmpDir, 0o755); err != nil {
		return fmt.Errorf("telegram: create tmp dir %s: %w", b.TmpDir, err)
	}
	b.dedup = newDedupe(2 * time.Minute)
	b.cm = cinemeta.NewClient()
	client, err := gotgproto.NewClient(
		b.AppID, b.APIHash,
		gotgproto.ClientTypeBot(b.BotToken),
		&gotgproto.ClientOpts{InMemory: true, Session: sessionMaker.SimpleSession(), DisableCopyright: true},
	)
	if err != nil {
		return fmt.Errorf("telegram: start client: %w", err)
	}
	d := client.Dispatcher
	d.AddHandler(handlers.NewCommand("start", b.onStart))
	d.AddHandler(handlers.NewCommand("link", b.onLink))
	d.AddHandler(handlers.NewMessage(filters.Message.Media, b.onMedia))
	slog.Info("telegram bot started", "username", client.Self.Username)
	return client.Idle()
}

func (b *Bot) onStart(ctx *ext.Context, u *ext.Update) error {
	if tgUser := u.EffectiveUser(); tgUser != nil {
		if !b.dedup.firstSight(tgUser.ID, u.EffectiveMessage.ID) {
			return nil
		}
		if fields := strings.Fields(u.EffectiveMessage.Text); len(fields) > 1 {
			if uid, ok := b.Link(fields[1], tgUser.ID); ok {
				slog.Info("telegram: linked", "user", uid, "tg", tgUser.ID)
				_, err := ctx.Reply(u, ext.ReplyTextString("✅ Linked. Forward me a video to cache it."), nil)
				return err
			}
			_, err := ctx.Reply(u, ext.ReplyTextString("That link code was invalid or expired, grab a fresh one in Torrin settings."), nil)
			return err
		}
	}
	_, err := ctx.Reply(u, ext.ReplyTextString(
		"Forward me a video and I'll cache it to your Torrin library.\n\n"+
			"First link your account: grab a code in Torrin settings and send /link <code>.\n\n"+
			"Tip: add a caption with the title or imdb id (e.g. \"The Batman 2022\" or tt1877830) so it shows up in Stremio search."), nil)
	return err
}

func (b *Bot) onLink(ctx *ext.Context, u *ext.Update) error {
	tgUser := u.EffectiveUser()
	if tgUser == nil {
		return nil
	}
	if !b.dedup.firstSight(tgUser.ID, u.EffectiveMessage.ID) {
		return nil
	}
	fields := strings.Fields(u.EffectiveMessage.Text)
	if len(fields) < 2 {
		_, _ = ctx.Reply(u, ext.ReplyTextString("Usage: /link <code>, get one in Torrin settings"), nil)
		return nil
	}
	if uid, ok := b.Link(fields[1], tgUser.ID); ok {
		slog.Info("telegram: linked", "user", uid, "tg", tgUser.ID)
		_, _ = ctx.Reply(u, ext.ReplyTextString("✅ Linked. Forward me a video to cache it."), nil)
	} else {
		_, _ = ctx.Reply(u, ext.ReplyTextString("❌ That code is invalid or expired."), nil)
	}
	return nil
}

func (b *Bot) onMedia(ctx *ext.Context, u *ext.Update) error {
	tgUser := u.EffectiveUser()
	if tgUser == nil {
		return nil
	}
	if !b.dedup.firstSight(tgUser.ID, u.EffectiveMessage.ID) {
		return nil
	}
	userID, ok := b.Resolve(tgUser.ID)
	if !ok {
		_, _ = ctx.Reply(u, ext.ReplyTextString("Link your Torrin account first: send /link <code> (code in Torrin settings)."), nil)
		return nil
	}
	if b.Paid != nil && !b.Paid(userID) {
		_, _ = ctx.Reply(u, ext.ReplyTextString("Telegram uploads need a paid plan."), nil)
		return nil
	}

	doc := filters.GetDocument(u.EffectiveMessage)
	if doc == nil {
		return nil
	}

	maxBytes, maxConc := int64(0), 1
	if b.Plan != nil {
		maxBytes, maxConc = b.Plan(userID)
	}
	if maxBytes > 0 && doc.Size > maxBytes {
		_, _ = ctx.Reply(u, ext.ReplyTextString(fmt.Sprintf("❌ That file is ~%dGB, over your plan's %dGB limit.", doc.Size/1e9, maxBytes/1e9)), nil)
		return nil
	}
	if n, _ := b.Repo.ActiveCount(context.Background(), userID); n >= maxConc {
		_, _ = ctx.Reply(u, ext.ReplyTextString(fmt.Sprintf("⏳ You've got %d download(s) running already, wait, then resend.", maxConc)), nil)
		return nil
	}

	name, err := functions.GetMediaFileName(u.EffectiveMessage.Media)
	if err != nil || name == "" {
		name = "telegram_" + strconv.FormatInt(doc.ID, 10)
	}
	name = filepath.Base(name)

	if v := safety.Screen(name); v.Blocked {
		if v.Ban && b.Ban != nil {
			b.Ban(userID, v.Reason)
		}
		slog.Warn("telegram: blocked content", "user", userID, "name", name, "reason", v.Reason, "banned", v.Ban)
		_, _ = ctx.Reply(u, ext.ReplyTextString("❌ That file is blocked and can't be cached."), nil)
		return nil
	}

	cacheKey := docCacheKey(doc.ID)
	node := b.Node
	if b.Sizer != nil {
		node = cluster.TargetNode(context.Background(), b.Sizer, string(jobs.SourceTelegram), doc.Size)
	}
	store, node := pickStore(node, b.Node, b.Store, b.Overflow)
	job := &jobs.Job{
		UserID: userID, InfoHash: cacheKey, Name: name, Source: jobs.SourceTelegram, Node: node,
		Status: jobs.StatusDownloading, FileSize: doc.Size,
	}
	if err := b.Repo.Create(context.Background(), job); err != nil {
		slog.Error("telegram: create job", "err", err)
		_, _ = ctx.Reply(u, ext.ReplyTextString("❌ Couldn't start that, try again."), nil)
		return nil
	}
	_, _ = ctx.Reply(u, ext.ReplyTextString("⏳ Downloading "+name+" ..."), nil)

	media := u.EffectiveMessage.Media
	caption := strings.TrimSpace(u.EffectiveMessage.Text)
	go func() {
		tmp := filepath.Join(b.TmpDir, cacheKey+"_"+name)
		out, err := os.Create(tmp)
		if err == nil {
			pw := &progressWriter{w: out, total: doc.Size, report: jobs.ProgressReporter(context.Background(), b.Repo, job.ID)}
			_, err = ctx.DownloadMedia(media, ext.DownloadOutputStream{Writer: pw}, nil)
			out.Close()
		}
		if err != nil {
			os.Remove(tmp)
			slog.Warn("telegram: download failed", "name", name, "err", err)
			b.failJob(job, "download failed")
			_, _ = ctx.Reply(u, ext.ReplyTextString("❌ Download failed."), nil)
			return
		}
		defer os.Remove(tmp)

		f := sources.File{
			Name: name, Path: tmp, Size: doc.Size, CacheKey: cacheKey, Source: jobs.SourceTelegram,
		}
		tag := b.applyIdentity(context.Background(), job, caption, name)
		if err := sources.Ingest(context.Background(), store, b.Blobs, b.Cipher, b.Repo, f, job); err != nil {
			slog.Error("telegram: ingest failed", "name", name, "err", err)
			b.failJob(job, "caching failed")
			_, _ = ctx.Reply(u, ext.ReplyTextString("❌ Caching failed."), nil)
			return
		}
		if b.Bus != nil {
			b.Bus.Publish(events.JobComplete, events.Complete{JobID: job.ID, InfoHash: job.InfoHash, Node: job.Node})
		}
		slog.Info("telegram: cached", "user", userID, "name", name, "job", job.ID, "imdb", job.IMDBID)
		_, _ = ctx.Reply(u, ext.ReplyTextString("✅ Done, "+name+" is in your Torrin library."+tag), nil)
	}()
	return nil
}

type progressWriter struct {
	w       io.Writer
	total   int64
	written int64
	report  func(current, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.written += int64(n)
	p.report(p.written, p.total)
	return n, err
}

func (b *Bot) failJob(job *jobs.Job, reason string) {
	job.Status = jobs.StatusFailed
	job.Error = reason
	b.Repo.Update(context.Background(), job)
}

func pickStore(node, home string, primary sources.Store, overflow map[string]sources.Store) (sources.Store, string) {
	if node != home {
		if s := overflow[node]; s != nil {
			return s, node
		}
	}
	return primary, home
}

func docCacheKey(docID int64) string {
	sum := sha1.Sum([]byte("tg:" + strconv.FormatInt(docID, 10)))
	return hex.EncodeToString(sum[:])
}
