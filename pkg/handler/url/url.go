package url

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path"

	mw "github.com/nais/wonderwall/pkg/middleware"
	"github.com/nais/wonderwall/pkg/router/paths"
)

const (
	RedirectURLParameter        = "redirect"
	RedirectURLEncodedParameter = "redirect-encoded"
)

// CanonicalRedirect constructs a redirect URL that points back to the application.
func CanonicalRedirect(r *http.Request) string {
	ingressPath, ok := mw.PathFrom(r.Context())
	if len(ingressPath) == 0 || !ok {
		ingressPath = "/"
	}

	// 1. Default
	redirect := ingressPath

	// 2. Referer header is set
	referer := r.Referer()
	if len(referer) > 0 {
		redirect = referer
	}

	// 3. Redirect parameter is set
	redirectParam := r.URL.Query().Get(RedirectURLParameter)
	if len(redirectParam) > 0 {
		redirect = redirectParam
	}

	// 4. Redirect-encoded parameter is set
	redirectEncodedParam := r.URL.Query().Get(RedirectURLEncodedParameter)
	if len(redirectEncodedParam) > 0 {
		decodedBytes, err := base64.RawURLEncoding.DecodeString(redirectEncodedParam)
		if err == nil {
			redirect = string(decodedBytes)
		}
	}

	// Ensure URL isn't encoded
	redirect, err := url.QueryUnescape(redirect)
	if err != nil {
		return ingressPath
	}

	parsed, err := url.Parse(redirect)
	if err != nil {
		// Silently fall back to ingress path
		return ingressPath
	}

	// Strip scheme and host to avoid cross-domain redirects
	parsed.Scheme = ""
	parsed.Host = ""

	redirect = parsed.String()

	// Root path without trailing slash is empty
	if len(parsed.Path) == 0 {
		redirect = "/"
	}

	// Ensure that empty path redirections falls back to the ingress' context path if applicable
	if len(redirect) == 0 {
		redirect = ingressPath
	}

	return redirect
}

func LoginURL(prefix, redirectTarget string) string {
	u := new(url.URL)
	u.Path = path.Join(prefix, paths.OAuth2, paths.Login)

	v := url.Values{}
	v.Set(RedirectURLEncodedParameter, RedirectEncoded(redirectTarget))
	u.RawQuery = v.Encode()

	return u.String()
}

func RedirectEncoded(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func LoginCallbackURL(r *http.Request) (string, error) {
	return makeCallbackURL(r, paths.LoginCallback)
}

func LogoutCallbackURL(r *http.Request) (string, error) {
	return makeCallbackURL(r, paths.LogoutCallback)
}

func makeCallbackURL(r *http.Request, callbackPath string) (string, error) {
	match, found := mw.IngressFrom(r.Context())
	if !found {
		return "", fmt.Errorf("request host does not match any configured ingresses")
	}

	targetPath := path.Join(match.Path(), paths.OAuth2, callbackPath)

	targetUrl := url.URL{
		Host:   match.Host(),
		Path:   targetPath,
		Scheme: match.Scheme,
	}

	return targetUrl.String(), nil
}
