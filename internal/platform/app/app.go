package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	adminhttp "gorixpay/internal/admin/httpapi"
	"gorixpay/internal/announcement"
	"gorixpay/internal/certification"
	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	"gorixpay/internal/channelops"
	epaypage "gorixpay/internal/compat/epay/page"
	epaypayment "gorixpay/internal/compat/epay/payment"
	epayv2 "gorixpay/internal/compat/epay/v2"
	"gorixpay/internal/complaint"
	"gorixpay/internal/device"
	"gorixpay/internal/domainauth"
	financeservice "gorixpay/internal/finance/service"
	"gorixpay/internal/funding"
	"gorixpay/internal/groupshop"
	"gorixpay/internal/identity/auth"
	identityhttp "gorixpay/internal/identity/httpapi"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/invite"
	ledgerquery "gorixpay/internal/ledger/query"
	ledgerservice "gorixpay/internal/ledger/service"
	"gorixpay/internal/merchant/credential"
	merchanthttp "gorixpay/internal/merchant/httpapi"
	"gorixpay/internal/merchantgroup"
	epaynotification "gorixpay/internal/notification/epay"
	"gorixpay/internal/notify"
	"gorixpay/internal/oauth"
	"gorixpay/internal/outbox"
	paymentcallback "gorixpay/internal/payment/callback"
	"gorixpay/internal/payment/cashier"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentquery "gorixpay/internal/payment/query"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/health"
	"gorixpay/internal/platform/httpserver"
	"gorixpay/internal/platform/migrate"
	redisx "gorixpay/internal/platform/redis"
	"gorixpay/internal/platform/secret"
	pluginpayment "gorixpay/internal/plugin/payment"
	"gorixpay/internal/plugin/payment/alipay"
	epayplugin "gorixpay/internal/plugin/payment/epay"
	"gorixpay/internal/plugin/payment/generic"
	"gorixpay/internal/plugin/payment/huifu"
	"gorixpay/internal/plugin/payment/jeepay"
	"gorixpay/internal/plugin/payment/qqpay"
	sandboxplugin "gorixpay/internal/plugin/payment/sandbox"
	"gorixpay/internal/plugin/payment/unionpay"
	"gorixpay/internal/plugin/payment/usdt"
	"gorixpay/internal/plugin/payment/wechatpayv3"
	"gorixpay/internal/profitsharing"
	"gorixpay/internal/reporting"
	"gorixpay/internal/risk"
	"gorixpay/internal/security"
	"gorixpay/internal/settlement"
	"gorixpay/internal/transfer"
	"gorixpay/migrations"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type App struct {
	cfg          config.Config
	db           *gorm.DB
	router       *gin.Engine
	redis        *redisx.Client
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
	application := &App{cfg: cfg, db: db, router: router}

	// 主密钥可以缺省：cipher 为 nil 时，渠道与商户密钥类功能在使用点报错，
	// 登录、验证码、找回密码这些不依赖密钥的功能照常可用。cipher 必须在
	// security.New 之前加载，否则两步验证会拿到 nil cipher。
	var cipher *secret.Cipher
	if cfg.Security.MasterKeyFile != "" {
		key, err := secret.LoadKeyFile(cfg.Security.MasterKeyFile)
		if err != nil {
			application.closeResources()
			return nil, err
		}
		cipher, err = secret.NewCipher(key, "v1")
		if err != nil {
			application.closeResources()
			return nil, err
		}
	}
	securityService := security.New(db, cipher)
	authService := auth.NewService(db, sessions).WithChallenge(securityService)
	identityhttp.Register(router.Group("/api/v1"), authService, sessions)
	security.Register(router.Group("/api/v1"), identityhttp.Authenticate(sessions), securityService)
	notify.Register(router.Group("/api/v1"), db, sessions, cipher)

	redisClient, redisErr := redisx.Open(redisx.Config{
		Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, Prefix: cfg.Redis.Prefix,
	})
	if redisErr != nil {
		logger.Warn("redis unavailable, continuing without it", "error", redisErr)
		redisClient = nil
	}
	application.redis = redisClient

	if cipher != nil {
		credentials := credential.NewService(db, cipher, time.Now)
		channelConfigs := channelconfig.NewService(cipher)
		ledgerWriter := ledgerservice.New(db, time.Now)
		certService := certification.New(db, cipher, ledgerWriter, time.Now)
		domainService := domainauth.New(db, time.Now)
		inviteService := invite.New(db, ledgerWriter, cfg.HTTP.PublicURL, time.Now)
		riskService := risk.New(db, time.Now)
		fundingService := funding.New(db, ledgerWriter, cfg.HTTP.PublicURL)
		merchantGroups := merchantgroup.New(db, time.Now)
		profitSharing := profitsharing.New(db, ledgerWriter, time.Now).WithFeeResolver(merchantGroups)
		orders := paymentservice.New(db, time.Now).WithGuards(
			certService.Guard(), domainService.Guard(), riskService.Guard(), fundingService.Guard(),
		)
		// Hooks are attached once, after every hook owner exists: WithHooks
		// replaces the slice, so a second call would silently drop the earlier
		// hooks (funding, invite rebate, profit sharing).
		callbacks := paymentcallback.NewWithLedger(db, time.Now, ledgerWriter)

		plugins := pluginpayment.NewRegistry()
		if err := plugins.Register(epayplugin.New(nil)); err != nil {
			application.closeResources()
			return nil, err
		}
		for _, plugin := range []pluginpayment.Plugin{
			alipay.New(nil), wechatpayv3.New(nil), jeepay.New(nil), generic.New(nil), huifu.New(nil), usdt.New(nil),
			qqpay.New(nil), unionpay.New(nil),
		} {
			if err := plugins.Register(plugin); err != nil {
				application.closeResources()
				return nil, err
			}
		}
		sandboxEnabled := cfg.Sandbox.Enabled && cfg.Environment != "production"
		if sandboxEnabled {
			if err := plugins.Register(sandboxplugin.New()); err != nil {
				application.closeResources()
				return nil, err
			}
		}
		channels := channelrouter.New(db, plugins, nil)
		channelOps := channelops.New(db, cipher, time.Now)
		channelops.Mount(router.Group("/api/v1"), identityhttp.Authenticate(sessions), channelOps)
		channels = channels.WithRollResolver(channelOps)
		orchestrator := paymentorchestrator.NewConfigured(db, channels, time.Now, channelConfigs, cfg.HTTP.PublicURL)
		deviceService := device.Register(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions), device.Deps{Orders: orders, Starter: orchestrator})
		groupshop.Register(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions), ledgerWriter)
		callbacks = callbacks.WithHooks(fundingService, inviteService.Hook(), profitSharing.Hook(), deviceService.PaymentHook())
		oauthService := oauth.Register(router.Group("/api/v1"), db, sessions, cipher, cfg.HTTP.PublicURL)
		finance := financeservice.New(db, ledgerWriter, time.Now)
		transferService := transfer.New(db, ledgerWriter, time.Now)
		settleService := settlement.New(db, finance, transferService, merchantGroups.RateBp, time.Now)
		outboxQuery := outbox.NewQuery(db)

		merchant := merchanthttp.Register(router.Group("/api/v1/merchant"), sessions, credentials)
		merchanthttp.RegisterOrders(merchant, paymentquery.New(db))
		merchanthttp.RegisterFinance(merchant, finance)
		merchanthttp.RegisterProfile(merchant, db)

		adminGroup := router.Group("/api/v1/admin", identityhttp.Authenticate(sessions), identityhttp.RequireAdministrator())
		adminhttp.Register(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions), channelConfigs, ledgerquery.New(db), ledgerWriter, finance, outboxQuery)
		adminhttp.RegisterExtras(adminGroup, db)

		epaypayment.Register(router, db, credentials, orders, cfg.HTTP.PublicURL, orchestrator)
		paymentcallback.RegisterEpay(router, db, channelConfigs, callbacks)

		transfer.RegisterMerchant(merchant, transferService)
		transfer.RegisterAdmin(adminGroup, transferService)
		announcement.Register(router, merchant, adminGroup, announcement.New(db))

		certification.RegisterMerchant(merchant, certService)
		certification.RegisterAdmin(adminGroup, certService)
		domainauth.RegisterMerchant(merchant, domainService)
		domainauth.RegisterAdmin(adminGroup, domainService)
		invite.RegisterMerchant(merchant, inviteService)
		invite.RegisterAdmin(adminGroup, inviteService)
		risk.RegisterMerchant(merchant, riskService)
		risk.RegisterAdmin(adminGroup, riskService)
		funding.RegisterMerchant(merchant, fundingService)
		funding.RegisterAdmin(adminGroup, fundingService)
		adminhttp.RegisterPlugins(adminGroup, db, channelConfigs, plugins)
		paymentcallback.RegisterPlugins(router, db, channelConfigs, callbacks, plugins)
		merchantgroup.Register(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions))
		complaint.Register(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions))
		settlement.Register(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions), finance, transferService, merchantGroups.RateBp)
		reporting.Register(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions), merchantGroups.RateBp)
		profitsharing.Mount(router.Group("/api/v1"), identityhttp.Authenticate(sessions), profitSharing)

		signer, signerErr := epayv2.LoadSigner(ctx, db, cipher)
		if signerErr != nil {
			logger.Warn("load epay v2 platform signer", "error", signerErr)
			signer = nil
		}
		var replay epayv2.ReplayStore
		if redisClient != nil {
			replay = redisClient
		}
		v2Deps := epayv2.Deps{
			DB: db, Credentials: credentials, Orders: orders, Finance: finance,
			Transfers: transferService, Starter: orchestrator, SiteURL: cfg.HTTP.PublicURL,
			Cipher: cipher, Replay: replay,
		}
		if err := epayv2.Register(router, v2Deps); err != nil {
			application.closeResources()
			return nil, err
		}
		epayv2.RegisterMerchantRoutes(merchant, v2Deps)
		epaypayment.RegisterAPI(router, db, credentials, orders, finance)

		var limiter cashier.Limiter
		if redisClient != nil {
			limiter = redisClient
		}
		cashier.Register(router, cashier.Deps{
			DB: db, Orders: orders, Starter: orchestrator, Callbacks: callbacks,
			Limiter: limiter, SandboxEnabled: sandboxEnabled,
		})

		router.NoRoute(serveSPA(cfg.HTTP.PublicURL))
		notifier := epaynotification.NewSenderWithSigner(db, credentials, nil, signer)
		epaypage.Register(router, db, cfg.HTTP.PublicURL, notifier)
		worker := outbox.NewWorker(db, notifier, time.Now, outbox.Options{})
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
		settleService.Start(workerContext, time.Minute)
		go func() {
			ticker := time.NewTicker(30 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-workerContext.Done():
					return
				case <-ticker.C:
					if _, err := transferService.ExpireRedBatches(workerContext, time.Now().UTC(), 24*time.Hour); err != nil {
						logger.Warn("expire red packet batches", "error", err)
					}
					if oauthService != nil {
						if _, err := oauthService.PurgeExpired(workerContext); err != nil {
							logger.Warn("purge oauth login state", "error", err)
						}
					}
				}
			}
		}()
	}
	return application, nil
}

func (a *App) closeResources() {
	if a.redis != nil {
		_ = a.redis.Close()
		a.redis = nil
	}
	closeDatabase(a.db)
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
	if a.redis != nil {
		_ = a.redis.Close()
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
