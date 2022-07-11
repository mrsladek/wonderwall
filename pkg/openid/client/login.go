package client

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/nais/wonderwall/pkg/openid"
	"github.com/nais/wonderwall/pkg/openid/config"
	"github.com/nais/wonderwall/pkg/router/request"
	"github.com/nais/wonderwall/pkg/strings"
)

const (
	LocaleURLParameter        = "locale"
	SecurityLevelURLParameter = "level"
)

var (
	InvalidSecurityLevelError  = errors.New("InvalidSecurityLevel")
	InvalidLocaleError         = errors.New("InvalidLocale")
	InvalidLoginParameterError = errors.New("InvalidLoginParameter")

	// LoginParameterMapping maps incoming login parameters to OpenID Connect parameters
	LoginParameterMapping = map[string]string{
		LocaleURLParameter:        "ui_locales",
		SecurityLevelURLParameter: "acr_values",
	}
)

type Login interface {
	AuthCodeURL() string
	CanonicalRedirect() string
	CodeChallenge() string
	CodeVerifier() string
	Cookie() *openid.LoginCookie
	Nonce() string
	State() string
}

func NewLogin(c Client, r *http.Request) (Login, error) {
	params, err := newLoginParameters(c)
	if err != nil {
		return nil, fmt.Errorf("generating parameters: %w", err)
	}

	url, err := params.authCodeURL(r)
	if err != nil {
		return nil, fmt.Errorf("generating auth code url: %w", err)
	}

	redirect := request.CanonicalRedirectURL(r, c.config().Wonderwall().Ingress)
	cookie := params.cookie(redirect)

	return &login{
		authCodeURL:       url,
		canonicalRedirect: redirect,
		cookie:            cookie,
		params:            params,
	}, nil
}

type login struct {
	authCodeURL       string
	canonicalRedirect string
	cookie            *openid.LoginCookie
	params            *loginParameters
}

func (l login) CodeChallenge() string {
	return l.params.CodeChallenge
}

func (l login) CodeVerifier() string {
	return l.params.CodeVerifier
}

func (l login) Nonce() string {
	return l.params.Nonce
}

func (l login) State() string {
	return l.params.State
}

func (l login) AuthCodeURL() string {
	return l.authCodeURL
}

func (l login) CanonicalRedirect() string {
	return l.canonicalRedirect
}

func (l login) Cookie() *openid.LoginCookie {
	return l.cookie
}

type loginParameters struct {
	Client
	CodeVerifier  string
	CodeChallenge string
	Nonce         string
	State         string
}

func newLoginParameters(c Client) (*loginParameters, error) {
	codeVerifier, err := strings.GenerateBase64(64)
	if err != nil {
		return nil, fmt.Errorf("creating code verifier: %w", err)
	}

	nonce, err := strings.GenerateBase64(32)
	if err != nil {
		return nil, fmt.Errorf("creating nonce: %w", err)
	}

	state, err := strings.GenerateBase64(32)
	if err != nil {
		return nil, fmt.Errorf("creating state: %w", err)
	}

	return &loginParameters{
		Client:        c,
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge(codeVerifier),
		Nonce:         nonce,
		State:         state,
	}, nil
}

func (in *loginParameters) authCodeURL(r *http.Request) (string, error) {
	scopes := in.config().Client().GetScopes().String()
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("scope", scopes),
		oauth2.SetAuthURLParam("nonce", in.Nonce),
		oauth2.SetAuthURLParam("response_mode", "query"),
		oauth2.SetAuthURLParam("code_challenge", in.CodeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}

	if in.config().Wonderwall().Loginstatus.NeedsResourceIndicator() {
		opts = append(opts, oauth2.SetAuthURLParam("resource", in.config().Wonderwall().Loginstatus.ResourceIndicator))
	}

	opts, err := in.withSecurityLevel(r, opts)
	if err != nil {
		return "", fmt.Errorf("%w: %+v", InvalidSecurityLevelError, err)
	}

	opts, err = in.withLocale(r, opts)
	if err != nil {
		return "", fmt.Errorf("%w: %+v", InvalidLocaleError, err)
	}

	authCodeUrl := in.oAuth2Config().AuthCodeURL(in.State, opts...)
	return authCodeUrl, nil
}

func (in *loginParameters) cookie(redirect string) *openid.LoginCookie {
	return &openid.LoginCookie{
		State:        in.State,
		Nonce:        in.Nonce,
		CodeVerifier: in.CodeVerifier,
		Referer:      redirect,
	}
}

func (in *loginParameters) withLocale(r *http.Request, opts []oauth2.AuthCodeOption) ([]oauth2.AuthCodeOption, error) {
	return withParamMapping(r,
		opts,
		LocaleURLParameter,
		in.config().Client().GetUILocales(),
		in.config().Provider().UILocalesSupported,
	)
}

func (in *loginParameters) withSecurityLevel(r *http.Request, opts []oauth2.AuthCodeOption) ([]oauth2.AuthCodeOption, error) {
	return withParamMapping(r,
		opts,
		SecurityLevelURLParameter,
		in.config().Client().GetACRValues(),
		in.config().Provider().ACRValuesSupported,
	)
}

func withParamMapping(r *http.Request, opts []oauth2.AuthCodeOption, param, fallback string, supported config.Supported) ([]oauth2.AuthCodeOption, error) {
	if len(fallback) == 0 {
		return opts, nil
	}

	value, err := LoginURLParameter(r, param, fallback, supported)
	if err != nil {
		return nil, err
	}

	opts = append(opts, oauth2.SetAuthURLParam(LoginParameterMapping[param], value))
	return opts, nil
}

// LoginURLParameter attempts to get a given parameter from the given HTTP request, falling back if none found.
// The value must exist in the supplied list of supported values.
func LoginURLParameter(r *http.Request, parameter, fallback string, supported config.Supported) (string, error) {
	value := r.URL.Query().Get(parameter)

	if len(value) == 0 {
		value = fallback
	}

	if supported.Contains(value) {
		return value, nil
	}

	return value, fmt.Errorf("%w: invalid value for %s=%s", InvalidLoginParameterError, parameter, value)
}

func codeChallenge(codeVerifier string) string {
	hasher := sha256.New()
	hasher.Write([]byte(codeVerifier))
	codeVerifierHash := hasher.Sum(nil)

	return base64.RawURLEncoding.EncodeToString(codeVerifierHash)
}
