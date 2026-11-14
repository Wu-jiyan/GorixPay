package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeProvider struct {
	name         string
	authorizeURL string
	identity     Identity
	exchangeErr  error
	lastState    string
	lastCode     string
}

func (provider *fakeProvider) Name() string { return provider.name }

func (provider *fakeProvider) AuthorizeURL(state string) string {
	provider.lastState = state
	return provider.authorizeURL + "?state=" + state
}

func (provider *fakeProvider) Exchange(_ context.Context, code string) (Identity, error) {
	provider.lastCode = code
	if provider.exchangeErr != nil {
		return Identity{}, provider.exchangeErr
	}
	return provider.identity, nil
}

func newFakeProvider(name string) *fakeProvider {
	return &fakeProvider{
		name:         name,
		authorizeURL: "https://fake.example/authorize",
		identity:     Identity{OpenID: "open-" + name, UnionID: "union-" + name, Nickname: "昵称", Avatar: "https://cdn.example/a.png"},
	}
}

func TestWechatProviderExchange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			if request.URL.Query().Get("code") != "code-1" || request.URL.Query().Get("appid") != "wx-app" {
				http.Error(writer, "bad params", http.StatusBadRequest)
				return
			}
			_, _ = writer.Write([]byte(`{"access_token":"token-1","openid":"open-1","unionid":"union-1"}`))
		case "/userinfo":
			if request.URL.Query().Get("access_token") != "token-1" {
				http.Error(writer, "bad token", http.StatusBadRequest)
				return
			}
			_, _ = writer.Write([]byte(`{"openid":"open-1","nickname":"微信昵称","headimgurl":"https://cdn.example/wx.png"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	provider := newWechatProvider("wx-app", "wx-secret", "https://pay.example/api/v1/auth/oauth/wechat/callback")
	provider.tokenURL = server.URL + "/token"
	provider.userInfo = server.URL + "/userinfo"

	authorize := provider.AuthorizeURL("state-1")
	for _, fragment := range []string{"appid=wx-app", "scope=snsapi_login", "state=state-1", "#wechat_redirect"} {
		if !strings.Contains(authorize, fragment) {
			t.Fatalf("authorize url %q missing %q", authorize, fragment)
		}
	}
	identity, err := provider.Exchange(context.Background(), "code-1")
	if err != nil {
		t.Fatal(err)
	}
	if identity.OpenID != "open-1" || identity.UnionID != "union-1" || identity.Nickname != "微信昵称" || identity.Avatar != "https://cdn.example/wx.png" {
		t.Fatalf("identity=%+v", identity)
	}
	if _, err := provider.Exchange(context.Background(), "bad-code"); !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("bad code err=%v", err)
	}
}

func TestQQProviderExchange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			_, _ = writer.Write([]byte("access_token=qq-token&expires_in=7776000"))
		case "/me":
			_, _ = writer.Write([]byte(`callback( {"client_id":"qq-app","openid":"qq-open"} );`))
		case "/userinfo":
			if request.URL.Query().Get("openid") != "qq-open" {
				http.Error(writer, "bad openid", http.StatusBadRequest)
				return
			}
			_, _ = writer.Write([]byte(`{"ret":0,"nickname":"QQ昵称","figureurl_qq_1":"https://cdn.example/qq.png"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	provider := newQQProvider("qq-app", "qq-secret", "https://pay.example/callback")
	provider.tokenURL = server.URL + "/token"
	provider.openIDURL = server.URL + "/me"
	provider.userInfo = server.URL + "/userinfo"

	identity, err := provider.Exchange(context.Background(), "code")
	if err != nil {
		t.Fatal(err)
	}
	if identity.OpenID != "qq-open" || identity.Nickname != "QQ昵称" || identity.Avatar != "https://cdn.example/qq.png" {
		t.Fatalf("identity=%+v", identity)
	}
	if !strings.Contains(provider.AuthorizeURL("s"), "client_id=qq-app") {
		t.Fatal("authorize url missing client id")
	}
}

func TestDouyinProviderExchange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			_, _ = writer.Write([]byte(`{"data":{"access_token":"dy-token","open_id":"dy-open","union_id":"dy-union"},"message":"success"}`))
		case "/userinfo":
			_, _ = writer.Write([]byte(`{"data":{"nickname":"抖音昵称","avatar":"https://cdn.example/dy.png"},"message":"success"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	provider := newDouyinProvider("dy-app", "dy-secret", "https://pay.example/callback")
	provider.tokenURL = server.URL + "/token"
	provider.userInfo = server.URL + "/userinfo"

	identity, err := provider.Exchange(context.Background(), "code")
	if err != nil {
		t.Fatal(err)
	}
	if identity.OpenID != "dy-open" || identity.UnionID != "dy-union" || identity.Nickname != "抖音昵称" {
		t.Fatalf("identity=%+v", identity)
	}
	if !strings.Contains(provider.AuthorizeURL("s"), "client_key=dy-app") {
		t.Fatal("authorize url missing client key")
	}
}

func TestProviderNamesCoverSpec(t *testing.T) {
	names := ProviderNames()
	if len(names) != 3 {
		t.Fatalf("names=%v", names)
	}
	for _, expected := range []string{providerWechat, providerQQ, providerDouyin} {
		if !knownProvider(expected) {
			t.Fatalf("provider %q missing", expected)
		}
	}
	if knownProvider("github") {
		t.Fatal("unknown provider accepted")
	}
}
