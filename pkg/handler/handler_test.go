package handler_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nais/wonderwall/pkg/cookie"
	urlpkg "github.com/nais/wonderwall/pkg/handler/url"
	"github.com/nais/wonderwall/pkg/mock"
)

func TestHandler_Login(t *testing.T) {
	cfg := mock.Config()
	idp := mock.NewIdentityProvider(cfg)
	defer idp.Close()

	rpClient := idp.RelyingPartyClient()

	resp := localLogin(t, rpClient, idp)
	loginURL := resp.Location

	req := idp.GetRequest(idp.RelyingPartyServer.URL + "/oauth2/logout")

	expectedCallbackURL, err := urlpkg.CallbackURL(req)
	assert.NoError(t, err)

	assert.Equal(t, idp.ProviderServer.URL, fmt.Sprintf("%s://%s", loginURL.Scheme, loginURL.Host))
	assert.Equal(t, "/authorize", loginURL.Path)
	assert.Equal(t, idp.OpenIDConfig.Client().ACRValues(), loginURL.Query().Get("acr_values"))
	assert.Equal(t, idp.OpenIDConfig.Client().UILocales(), loginURL.Query().Get("ui_locales"))
	assert.Equal(t, idp.OpenIDConfig.Client().ClientID(), loginURL.Query().Get("client_id"))
	assert.Equal(t, expectedCallbackURL, loginURL.Query().Get("redirect_uri"))
	assert.Equal(t, "S256", loginURL.Query().Get("code_challenge_method"))
	assert.ElementsMatch(t, idp.OpenIDConfig.Client().Scopes(), strings.Split(loginURL.Query().Get("scope"), " "))
	assert.NotEmpty(t, loginURL.Query().Get("state"))
	assert.NotEmpty(t, loginURL.Query().Get("nonce"))
	assert.NotEmpty(t, loginURL.Query().Get("code_challenge"))

	resp = get(t, rpClient, loginURL.String())
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)

	callbackURL := resp.Location
	assert.Equal(t, loginURL.Query().Get("state"), callbackURL.Query().Get("state"))
	assert.NotEmpty(t, callbackURL.Query().Get("code"))
}

func TestHandler_Callback(t *testing.T) {
	cfg := mock.Config()
	idp := mock.NewIdentityProvider(cfg)
	defer idp.Close()

	rpClient := idp.RelyingPartyClient()
	login(t, rpClient, idp)
}

func TestHandler_Logout(t *testing.T) {
	cfg := mock.Config()
	idp := mock.NewIdentityProvider(cfg)
	defer idp.Close()

	rpClient := idp.RelyingPartyClient()
	login(t, rpClient, idp)

	resp := localLogout(t, rpClient, idp)

	// Get endsession endpoint after local logout
	endsessionURL := resp.Location

	idpserverURL, err := url.Parse(idp.ProviderServer.URL)
	assert.NoError(t, err)

	req := idp.GetRequest(idp.RelyingPartyServer.URL + "/oauth2/logout")
	expectedLogoutCallbackURL, err := urlpkg.LogoutCallbackURL(req)
	assert.NoError(t, err)

	endsessionParams := endsessionURL.Query()
	assert.Equal(t, idpserverURL.Host, endsessionURL.Host)
	assert.Equal(t, "/endsession", endsessionURL.Path)
	assert.Equal(t, []string{expectedLogoutCallbackURL}, endsessionParams["post_logout_redirect_uri"])
	assert.NotEmpty(t, endsessionParams["id_token_hint"])
}

func TestHandler_LogoutCallback(t *testing.T) {
	cfg := mock.Config()
	idp := mock.NewIdentityProvider(cfg)
	defer idp.Close()

	rpClient := idp.RelyingPartyClient()
	login(t, rpClient, idp)
	logout(t, rpClient, idp)
}

func TestHandler_FrontChannelLogout(t *testing.T) {
	cfg := mock.Config()
	idp := mock.NewIdentityProvider(cfg)
	idp.OpenIDConfig.TestProvider.WithFrontChannelLogoutSupport()
	defer idp.Close()

	rpClient := idp.RelyingPartyClient()
	sessionCookie := login(t, rpClient, idp)

	// Trigger front-channel logout
	sid := func() string {
		ciphertext, err := base64.StdEncoding.DecodeString(sessionCookie.Value)
		assert.NoError(t, err)

		localSessionID, err := idp.RelyingPartyHandler.Crypter.Decrypt(ciphertext)
		assert.NoError(t, err)

		encryptedSession, err := idp.RelyingPartyHandler.Sessions.Read(context.Background(), string(localSessionID))
		assert.NoError(t, err)

		data, err := encryptedSession.Decrypt(idp.RelyingPartyHandler.Crypter)
		assert.NoError(t, err)

		return data.ExternalSessionID
	}()

	frontchannelLogoutURL, err := url.Parse(idp.RelyingPartyServer.URL)
	assert.NoError(t, err)

	frontchannelLogoutURL.Path = "/oauth2/logout/frontchannel"

	values := url.Values{}
	values.Add("sid", sid)
	values.Add("iss", idp.OpenIDConfig.Provider().Issuer())
	frontchannelLogoutURL.RawQuery = values.Encode()

	resp := get(t, rpClient, frontchannelLogoutURL.String())
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandler_SessionStateRequired(t *testing.T) {
	cfg := mock.Config()
	idp := mock.NewIdentityProvider(cfg)
	idp.OpenIDConfig.TestProvider.WithCheckSessionIFrameSupport(idp.ProviderServer.URL + "/checksession")
	defer idp.Close()

	rpClient := idp.RelyingPartyClient()

	resp := authorize(t, rpClient, idp)

	// Get callback URL after successful auth
	params := resp.Location.Query()
	sessionState := params.Get("session_state")
	assert.NotEmpty(t, sessionState)
}

func TestHandler_Default(t *testing.T) {
	up := newUpstream(t)
	defer up.Server.Close()

	t.Run("without auto-login", func(t *testing.T) {
		cfg := mock.Config()
		cfg.UpstreamHost = up.URL.Host
		idp := mock.NewIdentityProvider(cfg)
		defer idp.Close()

		up.SetReverseProxyUrl(idp.RelyingPartyServer.URL)
		rpClient := idp.RelyingPartyClient()

		// initial request without session
		resp := get(t, rpClient, idp.RelyingPartyServer.URL)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.Equal(t, "not ok", resp.Body)

		// acquire session
		login(t, rpClient, idp)

		// retry request with session
		resp = get(t, rpClient, idp.RelyingPartyServer.URL)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "ok", resp.Body)
	})

	t.Run("with auto-login", func(t *testing.T) {
		cfg := mock.Config()
		cfg.AutoLogin = true
		cfg.UpstreamHost = up.URL.Host
		idp := mock.NewIdentityProvider(cfg)
		defer idp.Close()

		up.SetReverseProxyUrl(idp.RelyingPartyServer.URL)
		rpClient := idp.RelyingPartyClient()

		// initial request without session
		target := idp.RelyingPartyServer.URL + "/"

		resp := get(t, rpClient, target)
		assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)

		// redirect should point to local login endpoint
		loginLocation := resp.Location
		assert.Equal(t, idp.RelyingPartyServer.URL+"/oauth2/login?redirect=/", loginLocation.String())

		// follow redirect to local login endpoint
		resp = get(t, rpClient, loginLocation.String())
		assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)

		// redirect should point to identity provider
		authorizeLocation := resp.Location

		authorizeEndpoint := *authorizeLocation
		authorizeEndpoint.RawQuery = ""
		assert.Equal(t, idp.OpenIDConfig.Provider().AuthorizationEndpoint(), authorizeEndpoint.String())

		// follow redirect to identity provider for login
		resp = get(t, rpClient, authorizeLocation.String())
		assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)

		// redirect should point back to relying party
		callbackLocation := resp.Location

		callbackEndpoint := *callbackLocation
		callbackEndpoint.RawQuery = ""

		req := idp.GetRequest(callbackLocation.String())
		expectedCallbackURL, err := urlpkg.CallbackURL(req)
		assert.NoError(t, err)
		assert.Equal(t, expectedCallbackURL, callbackEndpoint.String())

		// follow redirect back to relying party
		resp = get(t, rpClient, callbackLocation.String())
		assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)

		// finally, follow redirect back to original target, now with a session
		targetLocation := resp.Location
		assert.Equal(t, target, targetLocation.String())

		resp = get(t, rpClient, targetLocation.String())
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "ok", resp.Body)
	})

	t.Run("with auto-login and ignored paths", func(t *testing.T) {
		cfg := mock.Config()
		cfg.UpstreamHost = up.URL.Host
		cfg.AutoLogin = true
		cfg.AutoLoginIgnorePaths = []string{
			"/exact/match",
			"/allowed",
			"/wildcard/*",
			"/deeper/*/*",
			"/any*",
		}

		idp := mock.NewIdentityProvider(cfg)
		defer idp.Close()

		up.SetReverseProxyUrl(idp.RelyingPartyServer.URL)
		rpClient := idp.RelyingPartyClient()

		t.Run("matched paths should not trigger login", func(t *testing.T) {
			matched := []string{
				"/exact/match",
				"/allowed",
				"/wildcard/",
				"/wildcard/very",
				"/deeper/1/",
				"/deeper/1/2",
				"/anything",
				"/anywho",
			}
			for _, path := range matched {
				t.Run(path, func(t *testing.T) {
					target := idp.RelyingPartyServer.URL + path
					resp := get(t, rpClient, target)

					assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
					assert.Equal(t, "not ok", resp.Body)
				})
			}
		})

		t.Run("non-matched paths should trigger login", func(t *testing.T) {
			nonMatched := []string{
				"",
				"/",
				"/exact/match/",
				"/exact/match/huh",
				"/allowed/",
				"/not-allowed",
				"/not-allowed/allowed",
				"/wildcard",
				"/wildcard/yup/nope",
				"/deeper",
				"/deeper/",
				"/deeper/1",
				"/deeper/1/2/",
				"/deeper/1/2/3",
				"/any/",
				"/anywho/stvent",
			}
			for _, path := range nonMatched {
				t.Run(path, func(t *testing.T) {
					target := idp.RelyingPartyServer.URL + path
					resp := get(t, rpClient, target)

					assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
				})
			}
		})
	})
}

func localLogin(t *testing.T, rpClient *http.Client, idp *mock.IdentityProvider) response {
	// First, run /oauth2/login to set cookies
	loginURL, err := url.Parse(idp.RelyingPartyServer.URL + "/oauth2/login")
	assert.NoError(t, err)

	resp := get(t, rpClient, loginURL.String())
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)

	cookies := rpClient.Jar.Cookies(loginURL)
	sessionCookie := getCookieFromJar(cookie.Session, cookies)
	loginCookie := getCookieFromJar(cookie.Login, cookies)
	loginLegacyCookie := getCookieFromJar(cookie.LoginLegacy, cookies)

	assert.Nil(t, sessionCookie)
	assert.NotNil(t, loginCookie)
	assert.NotNil(t, loginLegacyCookie)

	return resp
}

func authorize(t *testing.T, rpClient *http.Client, idp *mock.IdentityProvider) response {
	resp := localLogin(t, rpClient, idp)

	// Follow redirect to authorize with identity provider
	resp = get(t, rpClient, resp.Location.String())
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)

	return resp
}

func callback(t *testing.T, rpClient *http.Client, authorizeResponse response) *http.Cookie {
	// Get callback URL after successful auth
	callbackURL := authorizeResponse.Location

	// Follow redirect to callback
	resp := get(t, rpClient, callbackURL.String())
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)

	cookies := rpClient.Jar.Cookies(callbackURL)
	sessionCookie := getCookieFromJar(cookie.Session, cookies)
	loginCookie := getCookieFromJar(cookie.Login, cookies)
	loginLegacyCookie := getCookieFromJar(cookie.LoginLegacy, cookies)

	assert.NotNil(t, sessionCookie)
	assert.Nil(t, loginCookie)
	assert.Nil(t, loginLegacyCookie)

	return sessionCookie
}

func login(t *testing.T, rpClient *http.Client, idp *mock.IdentityProvider) *http.Cookie {
	resp := authorize(t, rpClient, idp)
	return callback(t, rpClient, resp)
}

func localLogout(t *testing.T, rpClient *http.Client, idp *mock.IdentityProvider) response {
	// Request self-initiated logout
	logoutURL, err := url.Parse(idp.RelyingPartyServer.URL + "/oauth2/logout")
	assert.NoError(t, err)

	resp := get(t, rpClient, logoutURL.String())
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)

	cookies := rpClient.Jar.Cookies(logoutURL)
	sessionCookie := getCookieFromJar(cookie.Session, cookies)

	assert.Nil(t, sessionCookie)

	return resp
}

func logout(t *testing.T, rpClient *http.Client, idp *mock.IdentityProvider) {
	// Get endsession endpoint after local logout
	resp := localLogout(t, rpClient, idp)

	// Follow redirect to endsession endpoint at identity provider
	resp = get(t, rpClient, resp.Location.String())
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)

	// Get post-logout redirect URI after successful logout at identity provider
	logoutCallbackURI := resp.Location

	req := idp.GetRequest(resp.Location.String())
	expectedLogoutCallbackURL, err := urlpkg.LogoutCallbackURL(req)
	assert.NoError(t, err)

	assert.Contains(t, logoutCallbackURI.String(), expectedLogoutCallbackURL)
	assert.Equal(t, "/oauth2/logout/callback", logoutCallbackURI.Path)

	// Follow redirect back to logout callback
	resp = get(t, rpClient, logoutCallbackURI.String())
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)

	// Get post-logout redirect URI after redirect back to logout callback
	assert.Equal(t, "https://google.com", resp.Location.String())

	cookies := rpClient.Jar.Cookies(logoutCallbackURI)
	sessionCookie := getCookieFromJar(cookie.Session, cookies)

	assert.Nil(t, sessionCookie)
}

type response struct {
	Body       string
	Location   *url.URL
	StatusCode int
}

func get(t *testing.T, client *http.Client, url string) response {
	resp, err := client.Get(url)
	assert.NoError(t, err)
	defer resp.Body.Close()

	location, err := resp.Location()
	if !errors.Is(http.ErrNoLocation, err) {
		assert.NoError(t, err)
	}

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)

	return response{
		Body:       string(body),
		Location:   location,
		StatusCode: resp.StatusCode,
	}
}

type upstream struct {
	Server          *httptest.Server
	URL             *url.URL
	reverseProxyURL *url.URL
}

func (u *upstream) SetReverseProxyUrl(raw string) {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}

	u.reverseProxyURL = parsed
}

func newUpstream(t *testing.T) *upstream {
	u := new(upstream)

	upstreamHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")

		// Host should match the original authority from the ingress used to reach Wonderwall
		assert.Equal(t, u.reverseProxyURL.Host, r.Host)
		assert.NotEqual(t, u.URL.Host, r.Host)

		if len(token) > 0 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("not ok"))
		}
	})
	server := httptest.NewServer(upstreamHandler)

	upstreamURL, err := url.Parse(server.URL)
	assert.NoError(t, err)

	u.Server = server
	u.URL = upstreamURL
	return u
}

func getCookieFromJar(name string, cookies []*http.Cookie) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}

	return nil
}
