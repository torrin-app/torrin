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
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
	SignURL(path string, expiry time.Duration) string
	SignURLNode(node, path string, expiry time.Duration) string
	SignURLNodeUser(node, path, userID string, expiry time.Duration) string
	DeletePrefix(ctx context.Context, prefix string) error
}

type Publisher interface {
	Publish(subject string, v any) error
}

type Deps struct {
	Jobs    jobs.Repository
	JobsPG  *jobs.Postgres
	Users   *auth.Store
	Store   Storage
	Bus     Publisher
	Slots   *middleware.SlotTracker
	Qbit    *qbit.Client
	Scrape  *scrape.Client
	Mailer  *email.Client
	RClone  *rclonerc.Client
	Bitcart *billing.BitcartHandler
	Bachs   *billing.BachsHandler
	SignKey []byte
	Budget  int64

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
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("GET /api/plans", s.plans)
	mux.HandleFunc("GET /api/hosters", s.hosters)
	mux.HandleFunc("GET /api/sites", s.sites)
	mux.HandleFunc("POST /internal/view/{hash}", s.recordView)
	mux.HandleFunc("POST /api/scrape", s.scrapeHashes)
	mux.HandleFunc("POST /internal/prewarm", s.prewarm)
	mux.Handle("GET /api/me", authMW(http.HandlerFunc(s.me)))
	mux.Handle("POST /api/billing/crypto/checkout", authMW(http.HandlerFunc(s.cryptoCheckout)))
	mux.Handle("POST /api/billing/bachs/checkout", authMW(http.HandlerFunc(s.bachsCheckout)))
	mux.HandleFunc("GET /api/billing/crypto/invoice/{id}", s.cryptoInvoice)
	mux.HandleFunc("POST /api/billing/crypto/invoice/{id}/address", s.cryptoInvoiceAddress)
	mux.Handle("POST /api/jobs", authMW(http.HandlerFunc(s.submitJob)))
	mux.Handle("GET /api/jobs", authMW(http.HandlerFunc(s.listJobs)))
	mux.Handle("GET /api/jobs/{id}", authMW(http.HandlerFunc(s.getJob)))
	mux.Handle("GET /api/jobs/{id}/progress", authMW(http.HandlerFunc(s.progressJob)))
	mux.Handle("GET /api/jobs/{id}/zip", authMW(http.HandlerFunc(s.jobZip)))
	mux.Handle("POST /api/jobs/{id}/retry", authMW(http.HandlerFunc(s.retryJob)))
	mux.Handle("DELETE /api/jobs/{id}", authMW(http.HandlerFunc(s.deleteJob)))
	mux.Handle("POST /api/jobs/hoster", authMW(http.HandlerFunc(s.submitHoster)))
	mux.Handle("GET /api/search", authMW(http.HandlerFunc(s.search)))
	s.registerCredRoutes(mux, authMW, "/api/rd/credentials", credOps{s.Users.GetRDKey, s.Users.SetRDKey, s.Users.DeleteRDKey, providers.ValidateRD})
	s.registerCredRoutes(mux, authMW, "/api/alldebrid/credentials", credOps{s.Users.GetADKey, s.Users.SetADKey, s.Users.DeleteADKey, providers.ValidateAD})
	s.registerCredRoutes(mux, authMW, "/api/premiumize/credentials", credOps{s.Users.GetPMKey, s.Users.SetPMKey, s.Users.DeletePMKey, providers.ValidatePM})
	s.registerCredRoutes(mux, authMW, "/api/torbox/credentials", credOps{s.Users.GetTBKey, s.Users.SetTBKey, s.Users.DeleteTBKey, providers.ValidateTB})
	s.registerLibraryRoutes(mux, authMW)
	s.registerAdminRoutes(mux)
	s.registerUsenetRoutes(mux, authMW)
	s.registerUsenetSearchRoutes(mux, authMW)
	s.registerHDEncodeRoutes(mux, authMW)
	s.registerScenerlsRoutes(mux, authMW)
	s.registerStorageRoutes(mux, authMW)
	s.registerRSSRoutes(mux, authMW)
	s.registerStatsRoutes(mux, authMW)
	s.registerTelegramRoutes(mux, authMW)
	s.registerAccountRoutes(mux, authMW)
	s.registerResellerRoutes(mux, authMW)
	s.registerAvailabilityRoutes(mux, authMW)
	s.registerRcloneStreamRoutes(mux)
	s.registerStorageOAuthRoutes(mux, authMW)
}
