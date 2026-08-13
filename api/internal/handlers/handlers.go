package handlers

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/billing"
	"github.com/torrin-app/torrin/shared/email"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/providers"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/rclonerc"
	"github.com/torrin-app/torrin/shared/scrape"
)

type Storage interface {
	Has(ctx context.Context, key string) (bool, error)
	GetBytes(ctx context.Context, key string) ([]byte, error)
	GetReader(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
	SignURL(path string, expiry time.Duration) string
	SignURLNode(node, path string, expiry time.Duration) string
	SignURLNodeUser(node, path, userID string, expiry time.Duration) string
	DeletePrefix(ctx context.Context, prefix string) error
	Delete(ctx context.Context, key string) error
}

type Publisher interface {
	Publish(subject string, v any) error
}

type Deps struct {
	Jobs     jobs.Repository
	JobsPG   *jobs.Postgres
	Users    *auth.Store
	Store    Storage
	Bus      Publisher
	Slots    *middleware.SlotTracker
	Qbit     *qbit.Client
	QbitSeed *qbit.Client
	Scrape   *scrape.Client
	Mailer   *email.Client
	RClone   *rclonerc.Client
	Bitcart  *billing.BitcartHandler
	Bachs    *billing.BachsHandler
	SignKey  []byte
	Budget   int64

	SeedingEnabled    bool
	SeedingAllowUsers map[string]bool

	Internal      string
	AdminKey      string
	IndexerURL    string
	IndexerKey    string
	TGBotUsername string
	ResellerKey   string
	APIBase       string
	WebBase       string

	PrewarmMaxBytes  int64
	PrewarmMaxActive int
	PrewarmCapBytes  int64
}

type Server struct {
	Deps
}

func New(d Deps) *Server { return &Server{d} }

func (s *Server) Register(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	loginMW := func(h http.Handler) http.Handler { return authMW(middleware.RequireLogin(h)) }
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("GET /r/{code}", s.referralRedirect)
	mux.HandleFunc("GET /api/partner/report", s.partnerReport)
	mux.HandleFunc("GET /api/plans", s.plans)
	mux.HandleFunc("GET /api/hosters", s.hosters)
	mux.HandleFunc("GET /api/sites", s.sites)
	mux.HandleFunc("POST /internal/view/{hash}", s.recordView)
	mux.HandleFunc("POST /api/scrape", s.scrapeHashes)
	mux.HandleFunc("POST /internal/prewarm", s.prewarm)
	mux.Handle("GET /api/me", loginMW(http.HandlerFunc(s.me)))
	mux.Handle("GET /api/keys", loginMW(http.HandlerFunc(s.listKeys)))
	mux.Handle("POST /api/keys", loginMW(http.HandlerFunc(s.createKey)))
	mux.Handle("DELETE /api/keys/{id}", loginMW(http.HandlerFunc(s.revokeKey)))
	mux.Handle("POST /api/billing/crypto/checkout", loginMW(http.HandlerFunc(s.cryptoCheckout)))
	mux.Handle("POST /api/billing/bachs/checkout", loginMW(http.HandlerFunc(s.bachsCheckout)))
	s.registerAdminRoutes(mux)
	mux.HandleFunc("GET /api/billing/crypto/invoice/{id}", s.cryptoInvoice)
	mux.HandleFunc("POST /api/billing/crypto/invoice/{id}/address", s.cryptoInvoiceAddress)
	mux.Handle("POST /api/jobs", authMW(http.HandlerFunc(s.submitJob)))
	mux.Handle("POST /api/add", authMW(http.HandlerFunc(s.addJob)))
	mux.Handle("GET /api/jobs", authMW(http.HandlerFunc(s.listJobs)))
	mux.Handle("GET /api/jobs/{id}", authMW(http.HandlerFunc(s.getJob)))
	mux.Handle("GET /api/jobs/{id}/progress", authMW(http.HandlerFunc(s.progressJob)))
	mux.Handle("GET /api/jobs/{id}/zip", authMW(http.HandlerFunc(s.jobZip)))
	mux.Handle("POST /api/jobs/{id}/retry", authMW(http.HandlerFunc(s.retryJob)))
	mux.Handle("POST /api/jobs/{id}/recheck", authMW(http.HandlerFunc(s.recheckJob)))
	mux.Handle("DELETE /api/jobs/{id}", authMW(http.HandlerFunc(s.deleteJob)))
	mux.Handle("POST /api/jobs/hoster", authMW(http.HandlerFunc(s.submitHoster)))
	mux.Handle("POST /api/torrents/upload", authMW(http.HandlerFunc(s.uploadTorrent)))
	mux.Handle("GET /api/search", authMW(http.HandlerFunc(s.search)))
	s.registerCredRoutes(mux, loginMW, "/api/rd/credentials", credOps{s.Users.GetRDKey, s.Users.SetRDKey, s.Users.DeleteRDKey, providers.ValidateRD})
	s.registerCredRoutes(mux, loginMW, "/api/alldebrid/credentials", credOps{s.Users.GetADKey, s.Users.SetADKey, s.Users.DeleteADKey, providers.ValidateAD})
	s.registerCredRoutes(mux, loginMW, "/api/premiumize/credentials", credOps{s.Users.GetPMKey, s.Users.SetPMKey, s.Users.DeletePMKey, providers.ValidatePM})
	s.registerCredRoutes(mux, loginMW, "/api/torbox/credentials", credOps{s.Users.GetTBKey, s.Users.SetTBKey, s.Users.DeleteTBKey, providers.ValidateTB})
	mux.Handle("GET /api/debrid/usage", authMW(http.HandlerFunc(s.debridUsage)))
	s.registerLibraryRoutes(mux, authMW)
	s.registerUsenetRoutes(mux, authMW)
	s.registerCairnRoutes(mux, authMW)
	s.registerUsenetSearchRoutes(mux, authMW)
	s.registerHDEncodeRoutes(mux, authMW)
	s.registerScenerlsRoutes(mux, authMW)
	s.registerStorageRoutes(mux, loginMW)
	s.registerRSSRoutes(mux, authMW)
	s.registerStatsRoutes(mux, authMW)
	s.registerTelegramRoutes(mux, authMW)
	s.registerAccountRoutes(mux, loginMW)
	s.registerResellerRoutes(mux, loginMW)
	s.registerAvailabilityRoutes(mux, authMW)
	s.registerStorageOAuthRoutes(mux, loginMW)
}
