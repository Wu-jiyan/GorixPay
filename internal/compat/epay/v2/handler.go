package v2

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/merchant/credential"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/secret"
	"gorixpay/internal/transfer"

	"gorixpay/internal/compat/epay/signing"
	financeservice "gorixpay/internal/finance/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	// TimestampTolerance is the accepted clock skew for V2 requests.
	TimestampTolerance = 300 * time.Second
)

// PaymentStarter duplicates the orchestrator entry point so the V2 package keeps
// a narrow dependency on payment orchestration.
type PaymentStarter interface {
	Start(context.Context, string) (paymentorchestrator.Result, error)
}

// optionStarter is implemented by starters that accept the per-request payment
// hints (method/device/sub_openid/auth_code); the orchestrator does.
type optionStarter interface {
	StartWithOptions(context.Context, string, paymentorchestrator.StartOptions) (paymentorchestrator.Result, error)
}

// Deps carries every collaborator the V2 protocol needs.
type Deps struct {
	DB          *gorm.DB
	Credentials *credential.Service
	Orders      *paymentservice.Service
	Finance     *financeservice.Service
	Transfers   *transfer.Service
	Starter     PaymentStarter
	SiteURL     string
	Cipher      *secret.Cipher
	Replay      ReplayStore
}

// Service implements the epay V2 protocol handlers.
type Service struct {
	deps    Deps
	siteURL string
	now     func() time.Time
	replay  ReplayStore

	replayOnce sync.Once
	signerOnce sync.Once
	signer     *Signer
	signerErr  error
}

// Register mounts /api/:class/:method for GET and POST and installs the shared
// dispatcher used by /api.php?s=class/method.
func Register(router *gin.Engine, deps Deps) error {
	if router == nil {
		return errors.New("gin router is required")
	}
	if deps.DB == nil {
		return errors.New("database is required")
	}
	service := New(deps)
	group := router.Group("/api")
	group.GET("/:class/:method", service.servePath)
	group.POST("/:class/:method", service.servePath)
	setDefault(service)
	return nil
}

// New builds the V2 service. Signer loading is lazy so construction never fails
// because of a transient database problem.
func New(deps Deps) *Service {
	return &Service{
		deps:    deps,
		siteURL: strings.TrimRight(deps.SiteURL, "/"),
		now:     time.Now,
		replay:  deps.Replay,
	}
}

var (
	defaultMu      sync.RWMutex
	defaultService *Service
)

func setDefault(service *Service) {
	defaultMu.Lock()
	defaultService = service
	defaultMu.Unlock()
}

func defaultHandler() *Service {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultService
}

// HandleLegacyQuery dispatches api.php?s=pay/query style requests. It returns
// false when no V2 dispatcher is installed so the caller can keep legacy rules.
func HandleLegacyQuery(ctx *gin.Context) bool {
	service := defaultHandler()
	if service == nil {
		return false
	}
	target := strings.TrimSpace(ctx.Query("s"))
	if target == "" {
		target = strings.TrimSpace(ctx.PostForm("s"))
	}
	if target == "" {
		return false
	}
	service.dispatch(ctx, target)
	return true
}

// HandlePathRequest dispatches /api/{class}/{method} requests. It returns false
// for unknown methods or when no V2 dispatcher is installed.
func HandlePathRequest(ctx *gin.Context) bool {
	service := defaultHandler()
	if service == nil {
		return false
	}
	const prefix = "/api/"
	path := ctx.Request.URL.Path
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	parts := strings.Split(strings.Trim(path[len(prefix):], "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	target := parts[0] + "/" + parts[1]
	if _, ok := methodTable[target]; !ok {
		return false
	}
	service.dispatch(ctx, target)
	return true
}

// methodFunc implements one V2 method.
type methodFunc func(*Service, *request)

var methodTable = map[string]methodFunc{
	"pay/submit":       (*Service).paySubmit,
	"pay/create":       (*Service).payCreate,
	"pay/query":        (*Service).payQuery,
	"pay/close":        (*Service).payClose,
	"pay/refund":       (*Service).payRefund,
	"pay/refundquery":  (*Service).payRefundQuery,
	"merchant/info":    (*Service).merchantInfo,
	"merchant/orders":  (*Service).merchantOrders,
	"transfer/submit":  (*Service).transferSubmit,
	"transfer/query":   (*Service).transferQuery,
	"transfer/proof":   (*Service).transferProof,
	"transfer/balance": (*Service).transferBalance,
}

type request struct {
	ctx      *gin.Context
	method   string
	values   map[string]string
	merchant identitymodel.Merchant
	pid      int64
}

func (req *request) value(name string) string {
	return strings.TrimSpace(req.values[name])
}

func (req *request) context() context.Context {
	return req.ctx.Request.Context()
}

func (service *Service) servePath(ctx *gin.Context) {
	class := ctx.Param("class")
	method := ctx.Param("method")
	if class == "" || method == "" {
		service.writeError(ctx, -5, "接口方法不存在")
		return
	}
	service.dispatch(ctx, class+"/"+method)
}

type apiError struct {
	code int
	msg  string
}

func (err *apiError) Error() string { return err.msg }

func errorf(code int, message string) *apiError {
	return &apiError{code: code, msg: message}
}

func (service *Service) dispatch(ctx *gin.Context, target string) {
	handler, ok := methodTable[target]
	if !ok {
		service.writeError(ctx, -5, "接口方法不存在")
		return
	}
	if err := ctx.Request.ParseForm(); err != nil {
		service.writeError(ctx, -4, "未传入任何参数")
		return
	}
	req, err := service.authenticate(ctx, target)
	if err != nil {
		service.writeError(ctx, err.code, err.msg)
		return
	}
	handler(service, req)
}

func (service *Service) authenticate(ctx *gin.Context, target string) (*request, *apiError) {
	values := requestValues(ctx, target)
	if len(values) == 0 {
		return nil, errorf(-4, "未传入任何参数")
	}
	// ApiHelper.php:47-51 only treats the request as "parameterised" when the pid
	// key itself is present; a request that carries other parameters but no pid at
	// all is still rejected with -4, while a present-but-empty pid is -1 below.
	pidText, present := values["pid"]
	if !present {
		return nil, errorf(-4, "未传入任何参数")
	}
	pid, err := strconv.ParseInt(strings.TrimSpace(pidText), 10, 64)
	if err != nil || pid <= 0 {
		return nil, errorf(-1, "商户ID不能为空")
	}
	merchant, apiErr := service.findMerchant(ctx.Request.Context(), pid)
	if apiErr != nil {
		return nil, apiErr
	}
	timestampRequested := strings.TrimSpace(values["timestamp"])
	if timestampRequested == "" {
		return nil, errorf(-3, "时间戳(timestamp)字段不能为空")
	}
	seconds, err := strconv.ParseInt(timestampRequested, 10, 64)
	if err != nil || len(timestampRequested) != 10 || absDuration(service.now().Sub(time.Unix(seconds, 0))) > TimestampTolerance {
		return nil, errorf(-3, "时间戳字段不正确，请检查服务器时间")
	}
	if !strings.EqualFold(strings.TrimSpace(values["sign_type"]), "RSA") {
		return nil, errorf(-3, "该接口只能使用RSA签名类型")
	}
	signature := strings.TrimSpace(values["sign"])
	if signature == "" {
		return nil, errorf(-3, "缺少签名参数")
	}
	resolved, resolveErr := service.deps.Credentials.ResolveRSA(ctx.Request.Context(), merchant.ID)
	if resolveErr != nil {
		return nil, errorf(-3, "商户未配置RSA密钥")
	}
	publicKey, parseErr := signing.ParseRSAPublicKeyPEM(resolved.PublicKey)
	if parseErr != nil {
		return nil, errorf(-3, "商户公钥格式不正确")
	}
	if !VerifyValuesRSA(values, publicKey, signature) {
		return nil, errorf(-3, "RSA签名校验失败")
	}
	if requiresOnce(target) {
		store := service.replayStore()
		accepted, onceErr := store.Once(ctx.Request.Context(), ReplayKey(merchant.ID, target, signature), NonceTTL)
		if onceErr != nil {
			return nil, errorf(-3, "请求校验失败")
		}
		if !accepted {
			return nil, errorf(-3, "重复请求")
		}
	}
	return &request{ctx: ctx, method: target, values: values, merchant: merchant, pid: pid}, nil
}

func (service *Service) findMerchant(ctx context.Context, pid int64) (identitymodel.Merchant, *apiError) {
	var merchant identitymodel.Merchant
	err := service.deps.DB.WithContext(ctx).Where("legacy_uid = ?", pid).First(&merchant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return identitymodel.Merchant{}, errorf(-1, "商户不存在！")
	}
	if err != nil {
		return identitymodel.Merchant{}, errorf(-1, "商户查询失败")
	}
	if merchant.Status != identitymodel.StatusActive {
		return identitymodel.Merchant{}, errorf(-1, "商户已被封禁")
	}
	return merchant, nil
}

func (service *Service) replayStore() ReplayStore {
	service.replayOnce.Do(func() {
		if service.replay == nil {
			// No Redis was configured: fall back to the durable nonce table so
			// replay protection never silently disappears.
			service.replay = NewDBReplayStore(service.deps.DB, service.now)
		}
	})
	return service.replay
}

func requiresOnce(target string) bool {
	switch target {
	case "pay/refund", "pay/close":
		return true
	}
	return strings.HasPrefix(target, "transfer/")
}

// requestValues mirrors ApiHelper::verify(): parameters are read from the form
// body only, never from the query string. Url query parameters are unsigned, so
// honouring them would let an attacker inject business parameters into an
// otherwise valid signature.
//
// pay/submit is the one documented exception: Pay.php:12-18 reads $_GET first and
// falls back to $_POST, which is how link style integrations call it. Only for
// that method are query parameters accepted (manifest: pay/submit.methods).
func requestValues(ctx *gin.Context, target string) map[string]string {
	if target == "pay/submit" {
		if query := ctx.Request.URL.Query(); query.Get("pid") != "" {
			return firstValues(query, "")
		}
	}
	return firstValues(ctx.Request.PostForm, "s")
}

// firstValues keeps the first value of every parameter, dropping the legacy
// dispatcher selector when it travels inside the payload.
func firstValues(source url.Values, skip string) map[string]string {
	values := make(map[string]string, len(source))
	for name, list := range source {
		if name == skip || len(list) == 0 {
			continue
		}
		values[name] = list[0]
	}
	return values
}

// writeJSON signs a method's own return array: ApiHelper.php:26-30 appends
// timestamp/sign_type to whatever the method returned and signs it, even when
// the business code is negative.
func (service *Service) writeJSON(ctx *gin.Context, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["timestamp"] = strconv.FormatInt(service.now().Unix(), 10)
	payload["sign_type"] = "RSA"
	if signature, err := service.signPayload(payload); err == nil {
		payload["sign"] = signature
	}
	ctx.JSON(http.StatusOK, payload)
}

// writeResultError reports a negative code the way a method returns one: the
// array is signed. Use it only where the reference implementation returns an
// array (Order::close, Order::refund, Transfer::add) instead of throwing.
func (service *Service) writeResultError(ctx *gin.Context, code int, message string) {
	service.writeJSON(ctx, map[string]any{"code": code, "msg": message})
}

// writeError mirrors echojsonmsg() (Epay/includes/functions.php:1430): a bare
// {"code":N,"msg":"..."} that is never signed and carries no timestamp, because
// ApiHelper.php:32-35 answers thrown exceptions outside the signing branch.
func (service *Service) writeError(ctx *gin.Context, code int, message string) {
	ctx.JSON(http.StatusOK, map[string]any{"code": code, "msg": message})
}

func (service *Service) signPayload(payload map[string]any) (string, error) {
	signer, err := service.loadSigner()
	if err != nil {
		return "", err
	}
	return signer.SignParams(payload)
}

func (service *Service) loadSigner() (*Signer, error) {
	service.signerOnce.Do(func() {
		service.signer, service.signerErr = LoadSigner(context.Background(), service.deps.DB, service.deps.Cipher)
	})
	if service.signerErr != nil {
		return nil, service.signerErr
	}
	return service.signer, nil
}

// Signer exposes the lazily loaded platform signer, mainly for wiring the V2
// notification sender.
func (service *Service) Signer() (*Signer, error) {
	return service.loadSigner()
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
