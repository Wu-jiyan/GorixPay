package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	adminhttp "gorixpay/internal/admin/httpapi"
	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	epaypayment "gorixpay/internal/compat/epay/payment"
	financeservice "gorixpay/internal/finance/service"
	"gorixpay/internal/identity/auth"
	identityhttp "gorixpay/internal/identity/httpapi"
	"gorixpay/internal/identity/session"
	ledgerquery "gorixpay/internal/ledger/query"
	ledgerservice "gorixpay/internal/ledger/service"
	"gorixpay/internal/merchant/credential"
	merchanthttp "gorixpay/internal/merchant/httpapi"
	epaynotification "gorixpay/internal/notification/epay"
	"gorixpay/internal/outbox"
	paymentcallback "gorixpay/internal/payment/callback"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentquery "gorixpay/internal/payment/query"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/health"
	"gorixpay/internal/platform/httpserver"
	"gorixpay/internal/platform/migrate"
	"gorixpay/internal/platform/secret"
	pluginpayment "gorixpay/internal/plugin/payment"
	epayplugin "gorixpay/internal/plugin/payment/epay"
	"gorixpay/migrations"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type App struct {
	cfg          config.Config
	db           *gorm.DB
	router       *gin.Engine
	workerCancel context.CancelFunc
	workerDone   <-chan struct{}
}

type databaseChecker struct {
	db *gorm.DB
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	db, _, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	if err := (migrate.Runner{DB: db, FS: migrations.Files}).Up(ctx); err != nil {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	mode := gin.ReleaseMode
	if cfg.Environment == "development" || cfg.Environment == "test" {
		mode = gin.TestMode
	}
	router := httpserver.NewRouter(logger, mode)
	health.Register(router, databaseChecker{db: db})
	sessions := session.NewService(db, time.Now)
	identityhttp.Register(router.Group("/api/v1"), auth.NewService(db, sessions), sessions)
	application := &App{cfg: cfg, db: db, router: router}
	if cfg.Security.MasterKeyFile != "" {
		key, err := secret.LoadKeyFile(cfg.Security.MasterKeyFile)
		if err != nil {
			closeDatabase(db)
			return nil, err
		}
		cipher, err := secret.NewCipher(key, "v1")
		if err != nil {
			closeDatabase(db)
			return nil, err
		}
		credentials := credential.NewService(db, cipher, time.Now)
		channelConfigs := channelconfig.NewService(cipher)
		plugins := pluginpayment.NewRegistry()
		if err := plugins.Register(epayplugin.New(nil)); err != nil {
			closeDatabase(db)
			return nil, err
		}
		channels := channelrouter.New(db, plugins, nil)
		orchestrator := paymentorchestrator.NewConfigured(db, channels, time.Now, channelConfigs, cfg.HTTP.PublicURL)
		ledgerWriter := ledgerservice.New(db, time.Now)
		finance := financeservice.New(db, ledgerWriter, time.Now)
		outboxQuery := outbox.NewQuery(db)
		merchant := merchanthttp.Register(router.Group("/api/v1/merchant"), sessions, credentials)
		merchanthttp.RegisterOrders(merchant, paymentquery.New(db))
		merchanthttp.RegisterFinance(merchant, finance)
		adminhttp.Register(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions), channelConfigs, ledgerquery.New(db), ledgerWriter, finance, outboxQuery)
		epaypayment.Register(router, db, credentials, paymentservice.New(db, time.Now), cfg.HTTP.PublicURL, orchestrator)
		paymentcallback.RegisterEpay(router, db, channelConfigs, paymentcallback.New(db, time.Now))
		router.NoRoute(serveSPA(cfg.HTTP.PublicURL))
		worker := outbox.NewWorker(db, epaynotification.NewSender(db, credentials, nil), time.Now, outbox.Options{})
		workerContext, workerCancel := context.WithCancel(context.Background())
		workerDone := make(chan struct{})
		application.workerCancel = workerCancel
		application.workerDone = workerDone
		go func() {
			defer close(workerDone)
			if err := worker.Run(workerContext); err != nil {
				logger.Error("run outbox worker", "error", err)
			}
		}()
	}
	return application, nil
}

func closeDatabase(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}

func (a *App) Handler() http.Handler {
	return a.router
}

func (a *App) DB() *gorm.DB {
	return a.db
}

func (a *App) Address() string {
	return a.cfg.HTTP.Address
}

func (a *App) Close() error {
	if a.workerCancel != nil {
		a.workerCancel()
		<-a.workerDone
	}
	sqlDB, err := a.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (checker databaseChecker) Check(ctx context.Context) error {
	sqlDB, err := checker.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func serveSPA(publicURL string) gin.HandlerFunc {
	distDir := "frontend/dist"
	fs := http.FileServer(http.Dir(distDir))

	return func(ctx *gin.Context) {
		path := ctx.Request.URL.Path
		if len(path) >= 4 && path[:4] == "/api" {
			ctx.Status(http.StatusNotFound)
			return
		}
		fullPath := filepath.Join(distDir, path)
		info, err := os.Stat(fullPath)
		if err != nil || info.IsDir() {
			ctx.Request.URL.Path = "/"
			fs.ServeHTTP(ctx.Writer, ctx.Request)
			return
		}
		fs.ServeHTTP(ctx.Writer, ctx.Request)
	}
}
