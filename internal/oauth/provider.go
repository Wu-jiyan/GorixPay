// Package oauth implements third party login (WeChat, QQ, Douyin) and QR code
// login. The provider implementations talk to the official endpoints; wiring a
// real deployment needs an application registered on each open platform, since
// app_id and app_secret must be issued by the platform. Without credentials the
// provider reports ErrNotConfigured instead of failing later in the flow.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrNotConfigured   = errors.New("oauth provider is not configured")
	ErrProviderUnknown = errors.New("oauth provider is unknown")
	ErrExchangeFailed  = errors.New("oauth exchange failed")
)

const (
	providerWechat = "wechat"
	providerQQ     = "qq"
	providerDouyin = "douyin"

	maxProviderResponse = 256 * 1024
	providerHTTPTimeout = 10 * time.Second
)

// Identity is the profile a provider returns for one authorized visitor.
type Identity struct {
	OpenID   string
	UnionID  string
	Nickname string
	Avatar   string
}

// Provider is one third party login source.
type Provider interface {
	Name() string
	AuthorizeURL(state string) string
	Exchange(ctx context.Context, code string) (Identity, error)
}

// ProviderNames lists the built-in providers.
func ProviderNames() []string {
	return []string{providerWechat, providerQQ, providerDouyin}
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: providerHTTPTimeout}
}

// wechatProvider implements WeChat Open Platform QR login.
type wechatProvider struct {
	appID      string
	appSecret  string
	redirect   string
	authorize  string
	tokenURL   string
	userInfo   string
	httpClient *http.Client
}

func newWechatProvider(appID, appSecret, redirect string) *wechatProvider {
	return &wechatProvider{
		appID: appID, appSecret: appSecret, redirect: redirect,
		authorize:  "https://open.weixin.qq.com/connect/qrconnect",
		tokenURL:   "https://api.weixin.qq.com/sns/oauth2/access_token",
		userInfo:   "https://api.weixin.qq.com/sns/userinfo",
		httpClient: newHTTPClient(),
	}
}

func (provider *wechatProvider) Name() string { return providerWechat }

func (provider *wechatProvider) AuthorizeURL(state string) string {
	values := url.Values{}
	values.Set("appid", provider.appID)
	values.Set("redirect_uri", provider.redirect)
	values.Set("response_type", "code")
	values.Set("scope", "snsapi_login")
	values.Set("state", state)
	return provider.authorize + "?" + values.Encode() + "#wechat_redirect"
}

func (provider *wechatProvider) Exchange(ctx context.Context, code string) (Identity, error) {
	tokenValues := url.Values{}
	tokenValues.Set("appid", provider.appID)
	tokenValues.Set("secret", provider.appSecret)
	tokenValues.Set("code", code)
	tokenValues.Set("grant_type", "authorization_code")
	var token struct {
		AccessToken string `json:"access_token"`
		OpenID      string `json:"openid"`
		UnionID     string `json:"unionid"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := getJSON(ctx, provider.httpClient, provider.tokenURL+"?"+tokenValues.Encode(), &token); err != nil {
		return Identity{}, err
	}
	if token.ErrCode != 0 || token.AccessToken == "" || token.OpenID == "" {
		return Identity{}, fmt.Errorf("%w: wechat token: %s", ErrExchangeFailed, token.ErrMsg)
	}
	infoValues := url.Values{}
	infoValues.Set("access_token", token.AccessToken)
	infoValues.Set("openid", token.OpenID)
	infoValues.Set("lang", "zh_CN")
	var profile struct {
		OpenID   string `json:"openid"`
		UnionID  string `json:"unionid"`
		Nickname string `json:"nickname"`
		Avatar   string `json:"headimgurl"`
		ErrCode  int    `json:"errcode"`
		ErrMsg   string `json:"errmsg"`
	}
	if err := getJSON(ctx, provider.httpClient, provider.userInfo+"?"+infoValues.Encode(), &profile); err != nil {
		return Identity{}, err
	}
	if profile.ErrCode != 0 {
		return Identity{}, fmt.Errorf("%w: wechat userinfo: %s", ErrExchangeFailed, profile.ErrMsg)
	}
	return Identity{
		OpenID:   token.OpenID,
		UnionID:  firstNonEmpty(token.UnionID, profile.UnionID),
		Nickname: profile.Nickname,
		Avatar:   profile.Avatar,
	}, nil
}

// qqProvider implements QQ Connect.
type qqProvider struct {
	appID      string
	appSecret  string
	redirect   string
	authorize  string
	tokenURL   string
	openIDURL  string
	userInfo   string
	httpClient *http.Client
}

func newQQProvider(appID, appSecret, redirect string) *qqProvider {
	return &qqProvider{
		appID: appID, appSecret: appSecret, redirect: redirect,
		authorize:  "https://graph.qq.com/oauth2.0/authorize",
		tokenURL:   "https://graph.qq.com/oauth2.0/token",
		openIDURL:  "https://graph.qq.com/oauth2.0/me",
		userInfo:   "https://graph.qq.com/user/get_user_info",
		httpClient: newHTTPClient(),
	}
}

func (provider *qqProvider) Name() string { return providerQQ }

func (provider *qqProvider) AuthorizeURL(state string) string {
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", provider.appID)
	values.Set("redirect_uri", provider.redirect)
	values.Set("state", state)
	return provider.authorize + "?" + values.Encode()
}

func (provider *qqProvider) Exchange(ctx context.Context, code string) (Identity, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("client_id", provider.appID)
	values.Set("client_secret", provider.appSecret)
	values.Set("code", code)
	values.Set("redirect_uri", provider.redirect)
	body, err := getBody(ctx, provider.httpClient, provider.tokenURL+"?"+values.Encode())
	if err != nil {
		return Identity{}, err
	}
	parsed, err := url.ParseQuery(strings.TrimSpace(body))
	if err != nil {
		return Identity{}, fmt.Errorf("%w: qq token response", ErrExchangeFailed)
	}
	accessToken := parsed.Get("access_token")
	if accessToken == "" {
		return Identity{}, fmt.Errorf("%w: qq token: %s", ErrExchangeFailed, parsed.Get("error_description"))
	}
	meBody, err := getBody(ctx, provider.httpClient, provider.openIDURL+"?access_token="+url.QueryEscape(accessToken))
	if err != nil {
		return Identity{}, err
	}
	var me struct {
		OpenID string `json:"openid"`
	}
	if err := json.Unmarshal([]byte(stripJSONP(meBody)), &me); err != nil || me.OpenID == "" {
		return Identity{}, fmt.Errorf("%w: qq openid", ErrExchangeFailed)
	}
	infoValues := url.Values{}
	infoValues.Set("access_token", accessToken)
	infoValues.Set("oauth_consumer_key", provider.appID)
	infoValues.Set("openid", me.OpenID)
	var profile struct {
		Ret      int    `json:"ret"`
		Msg      string `json:"msg"`
		Nickname string `json:"nickname"`
		Avatar   string `json:"figureurl_qq_1"`
	}
	if err := getJSON(ctx, provider.httpClient, provider.userInfo+"?"+infoValues.Encode(), &profile); err != nil {
		return Identity{}, err
	}
	if profile.Ret != 0 {
		return Identity{}, fmt.Errorf("%w: qq userinfo: %s", ErrExchangeFailed, profile.Msg)
	}
	return Identity{OpenID: me.OpenID, Nickname: profile.Nickname, Avatar: profile.Avatar}, nil
}

// douyinProvider implements Douyin Open Platform login.
type douyinProvider struct {
	appID      string
	appSecret  string
	redirect   string
	authorize  string
	tokenURL   string
	userInfo   string
	httpClient *http.Client
}

func newDouyinProvider(appID, appSecret, redirect string) *douyinProvider {
	return &douyinProvider{
		appID: appID, appSecret: appSecret, redirect: redirect,
		authorize:  "https://open.douyin.com/platform/oauth/connect/",
		tokenURL:   "https://open.douyin.com/oauth/access_token/",
		userInfo:   "https://open.douyin.com/oauth/userinfo/",
		httpClient: newHTTPClient(),
	}
}

func (provider *douyinProvider) Name() string { return providerDouyin }

func (provider *douyinProvider) AuthorizeURL(state string) string {
	values := url.Values{}
	values.Set("client_key", provider.appID)
	values.Set("response_type", "code")
	values.Set("scope", "user_info")
	values.Set("redirect_uri", provider.redirect)
	values.Set("state", state)
	return provider.authorize + "?" + values.Encode()
}

func (provider *douyinProvider) Exchange(ctx context.Context, code string) (Identity, error) {
	var token struct {
		Data struct {
			AccessToken string `json:"access_token"`
			OpenID      string `json:"open_id"`
			UnionID     string `json:"union_id"`
		} `json:"data"`
		Message string `json:"message"`
	}
	payload := map[string]string{
		"client_key":    provider.appID,
		"client_secret": provider.appSecret,
		"code":          code,
		"grant_type":    "authorization_code",
	}
	if err := postJSON(ctx, provider.httpClient, provider.tokenURL, payload, &token); err != nil {
		return Identity{}, err
	}
	if token.Data.AccessToken == "" || token.Data.OpenID == "" {
		return Identity{}, fmt.Errorf("%w: douyin token: %s", ErrExchangeFailed, token.Message)
	}
	var profile struct {
		Data struct {
			Nickname string `json:"nickname"`
			Avatar   string `json:"avatar"`
			UnionID  string `json:"union_id"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := postJSON(ctx, provider.httpClient, provider.userInfo, map[string]string{
		"access_token": token.Data.AccessToken,
		"open_id":      token.Data.OpenID,
	}, &profile); err != nil {
		return Identity{}, err
	}
	return Identity{
		OpenID:   token.Data.OpenID,
		UnionID:  firstNonEmpty(token.Data.UnionID, profile.Data.UnionID),
		Nickname: profile.Data.Nickname,
		Avatar:   profile.Data.Avatar,
	}, nil
}

func getJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	body, err := getBody(ctx, client, endpoint)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(body), target); err != nil {
		return fmt.Errorf("%w: invalid json response", ErrExchangeFailed)
	}
	return nil
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, payload map[string]string, target any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(encoded)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	body, err := doRequest(client, request)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(body), target); err != nil {
		return fmt.Errorf("%w: invalid json response", ErrExchangeFailed)
	}
	return nil
}

func getBody(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	return doRequest(client, request)
}

func doRequest(client *http.Client, request *http.Request) (string, error) {
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrExchangeFailed, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponse+1))
	if err != nil {
		return "", fmt.Errorf("%w: read response", ErrExchangeFailed)
	}
	if len(body) > maxProviderResponse {
		return "", fmt.Errorf("%w: response too large", ErrExchangeFailed)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: status %d", ErrExchangeFailed, response.StatusCode)
	}
	return string(body), nil
}

func stripJSONP(body string) string {
	trimmed := strings.TrimSpace(body)
	if start := strings.Index(trimmed, "("); start >= 0 {
		if end := strings.LastIndex(trimmed, ")"); end > start {
			return trimmed[start+1 : end]
		}
	}
	return trimmed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
