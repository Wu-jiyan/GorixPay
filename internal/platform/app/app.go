package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
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
	"gorixpay/internal/invitecode"
	ledgerquery "gorixpay/internal/ledger/query"
	ledgerservice "gorixpay/internal/ledger/service"
	"gorixpay/internal/merchant/credential"
	merchanthttp "gorixpay/internal/merchant/httpapi"
	"gorixpay/internal/merchantgroup"
	"gorixpay/internal/merchanttools"
	epaynotification "gorixpay/internal/notification/epay"
	"gorixpay/internal/notify"
	"gorixpay/internal/notifyprefs"
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
	"gorixpay/internal/platform/netguard"
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
	background   *backgroundGroup
}

// backgroundGroup owns every background worker so shutting down waits for them
// and a panic inside one cannot take the payment process down with it.
type backgroundGroup struct {
	wait   sync.WaitGroup
	logger *slog.Logger
}

func newBackgroundGroup(logger *slog.Logger) *backgroundGroup {
	return &backgroundGroup{logger: logger}
}

// Go starts one named worker with panic recovery.
func (group *backgroundGroup) Go(name string, run func()) {
	group.wait.Add(1)
	go func() {
		defer group.wait.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				if group.logger != nil {
					group.logger.Error("background worker panicked",
						"worker", name, "panic", recovered, "stack", string(debug.Stack()))
				}
			}
		}()
		run()
	}()
}

func (group *backgroundGroup) Wait() { group.wait.Wait() }

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
	// Cookie authenticated console API: refuse state changes announced by another
	// origin (SameSite=Lax is the first line, this is the second).
	router.Use(httpserver.OriginGuard(cfg.HTTP.PublicURL))
	if len(cfg.HTTP.TrustedProxies) > 0 {
		// Only the configured proxies may speak for the client. Everything else is
		// reported by its socket address, so X-Forwarded-For cannot be forged.
		if err := router.SetTrustedProxies(cfg.HTTP.TrustedProxies); err != nil {
			closeDatabase(db)
			return nil, fmt.Errorf("invalid trusted proxies: %w", err)
		}
	}
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
	inviteCodes := invitecode.New(db)
	authService := auth.NewService(db, sessions).WithChallenge(securityService).WithInviteCodes(inviteCodes)
	identityhttp.Register(
		router.Group("/api/v1"), authService, sessions,
		// Behind a TLS terminator the inbound hop is plain HTTP, so the cookie
		// only gets Secure when the operator declares an https public URL.
		identityhttp.WithSecureCookies(strings.HasPrefix(strings.ToLower(cfg.HTTP.PublicURL), "https://")),
	)
	security.Register(router.Group("/api/v1"), identityhttp.Authenticate(sessions), securityService)
	// Without a real SMS/mail sender the service prints the reset token so a
	// local deployment can finish the flow. Production must never log a working
	// reset token, even when the operator has not configured a sender yet.
	notifyService := notify.Register(
		router.Group("/api/v1"), db, sessions, cipher,
		notify.WithTokenLogging(cfg.Environment != "production"),
	)

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
			paymentservice.URLGuard{AllowPrivate: cfg.Security.AllowPrivateNotifyHosts},
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
		if sandboxEnabled && !netguard.IsLocalTarget(cfg.HTTP.PublicURL) {
			// /api/v1/sandbox/orders/{trade_no}/complete has no authentication: it
			// marks an order paid for anyone who can reach it. Loud warning only —
			// staging and conformance runs legitimately use a fake public URL with
			// the sandbox channel on.
			logger.Warn("sandbox channel is enabled on a non-loopback public URL: anyone who can reach /api/v1/sandbox/orders/{trade_no}/complete can mark orders paid",
				"public_url", cfg.HTTP.PublicURL,
				"fix", "set GORIXPAY_ENVIRONMENT=production or GORIXPAY_SANDBOX_ENABLED=false")
		}
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
		merchanttools.Register(router, merchanttools.Deps{
			DB:        db,
			Sessions:  identityhttp.Authenticate(sessions),
			PublicURL: cfg.HTTP.PublicURL,
			Orders:    orders,
			Starter:   orchestrator,
		})
		oauthService := oauth.Register(router.Group("/api/v1"), db, sessions, cipher, cfg.HTTP.PublicURL)
		finance := financeservice.New(db, ledgerWriter, time.Now)
		transferService := transfer.New(db, ledgerWriter, time.Now)
		settleService := settlement.New(db, finance, transferService, merchantGroups.RateBp, time.Now)
		outboxQuery := outbox.NewQuery(db)

		notifyPrefs := notifyprefs.New(db, logger).WithCipher(cipher)
		if notifyService != nil {
			notifyPrefs = notifyPrefs.WithWeChat(notifyService.WeChatSender())
		}
		notifyprefs.RegisterMerchant(router.Group("/api/v1/merchant", identityhttp.Authenticate(sessions)), notifyPrefs)
		merchant := merchanthttp.Register(router.Group("/api/v1/merchant"), sessions, credentials)
		merchanthttp.RegisterOrders(merchant, paymentquery.New(db))
		merchanthttp.RegisterFinance(merchant, finance)
		merchanthttp.RegisterProfile(merchant, db)

		adminGroup := router.Group("/api/v1/admin", identityhttp.Authenticate(sessions), identityhttp.RequireAdministrator())
		invitecode.RegisterAdmin(adminGroup, inviteCodes)
		reporting.RegisterMerchantExports(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions))
		adminhttp.Register(router.Group("/api/v1"), db, identityhttp.Authenticate(sessions), channelConfigs, ledgerquery.New(db), ledgerWriter, finance, outboxQuery, adminhttp.WithCipher(cipher))
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
		// Merchant notification endpoints are attacker controlled (anyone can
		// register an order): bound the round trip and refuse internal targets.
		notifyClient := netguard.Client(netguard.Options{
			Timeout:      time.Duration(cfg.Security.NotifyTimeoutSeconds) * time.Second,
			AllowPrivate: cfg.Security.AllowPrivateNotifyHosts,
		})
		notifier := epaynotification.NewSenderWithSigner(db, credentials, notifyClient, signer)
		epaypage.Register(router, db, cfg.HTTP.PublicURL, notifier, epaypage.WithCaptchaVerifier(securityService.VerifyCaptcha))
		worker := outbox.NewWorker(db, notifier, time.Now, outbox.Options{})
		workerContext, workerCancel := context.WithCancel(context.Background())
		background := newBackgroundGroup(logger)
		application.workerCancel = workerCancel
		application.background = background
		background.Go("outbox", func() {
			if err := worker.Run(workerContext); err != nil {
				logger.Error("run outbox worker", "error", err)
			}
		})
		background.Go("settlement", func() {
			settleService.Run(workerContext, time.Minute)
		})
		complaintService := complaint.New(db, time.Now)
		background.Go("complaint-cron", func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-workerContext.Done():
					return
				case <-ticker.C:
					if replied, closed, err := complaintService.RunAutoRules(workerContext, time.Now()); err != nil {
						logger.Warn("complaint auto rules", "error", err)
					} else if replied > 0 || closed > 0 {
						logger.Info("complaint auto rules applied", "replied", replied, "closed", closed)
					}
					if failures := pluginpayment.RunCronHooks(workerContext, plugins.CronHooks()); len(failures) > 0 {
						logger.Warn("plugin cron hooks failed", "errors", failures)
					}
				}
			}
		})
		background.Go("red-packet-expiry", func() {
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
		})
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
	}
	// Wait for every background worker before closing the pool: a worker that is
	// still querying when the database closes fails or panics.
	if a.background != nil {
		a.background.Wait()
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
