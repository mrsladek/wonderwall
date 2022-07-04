package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nais/wonderwall/pkg/cookie"
	"github.com/nais/wonderwall/pkg/openid"
	logentry "github.com/nais/wonderwall/pkg/router/middleware"
)

const (
	LoginCookieLifetime = 1 * time.Hour
)

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	login, err := h.Client.Login(r)
	if err != nil {
		if errors.Is(err, openid.InvalidSecurityLevelError) || errors.Is(err, openid.InvalidLocaleError) {
			h.BadRequest(w, r, err)
		} else {
			h.InternalError(w, r, err)
		}

		return
	}

	err = h.setLoginCookies(w, login.Cookie())
	if err != nil {
		h.InternalError(w, r, fmt.Errorf("login: setting cookie: %w", err))
		return
	}

	fields := map[string]interface{}{
		"redirect_after_login": login.CanonicalRedirect(),
	}
	logger := logentry.LogEntryWithFields(r.Context(), fields)
	logger.Info().Msg("login: redirecting to identity provider")

	http.Redirect(w, r, login.AuthCodeURL(), http.StatusTemporaryRedirect)
}

func (h *Handler) getLoginCookie(r *http.Request) (*openid.LoginCookie, error) {
	loginCookieJson, err := cookie.GetDecrypted(r, cookie.Login, h.Crypter)
	if err != nil {
		logger := logentry.LogEntry(r.Context())
		logger.Info().Msgf("failed to fetch login cookie: %+v; falling back to legacy cookie", err)

		loginCookieJson, err = cookie.GetDecrypted(r, cookie.LoginLegacy, h.Crypter)
		if err != nil {
			return nil, err
		}
	}

	var loginCookie openid.LoginCookie
	err = json.Unmarshal([]byte(loginCookieJson), &loginCookie)
	if err != nil {
		return nil, fmt.Errorf("unmarshalling: %w", err)
	}

	return &loginCookie, nil
}

func (h *Handler) setLoginCookies(w http.ResponseWriter, loginCookie *openid.LoginCookie) error {
	loginCookieJson, err := json.Marshal(loginCookie)
	if err != nil {
		return fmt.Errorf("marshalling login cookie: %w", err)
	}

	opts := h.CookieOptions.
		WithExpiresIn(LoginCookieLifetime).
		WithSameSite(http.SameSiteNoneMode)
	value := string(loginCookieJson)

	err = cookie.EncryptAndSet(w, cookie.Login, value, opts, h.Crypter)
	if err != nil {
		return err
	}

	// set a duplicate cookie without the SameSite value set for user agents that do not properly handle SameSite
	err = cookie.EncryptAndSet(w, cookie.LoginLegacy, value, opts.WithSameSite(http.SameSiteDefaultMode), h.Crypter)
	if err != nil {
		return err
	}

	return nil
}

func (h *Handler) clearLoginCookies(w http.ResponseWriter) {
	opts := h.CookieOptions
	cookie.Clear(w, cookie.Login, opts.WithSameSite(http.SameSiteNoneMode))
	cookie.Clear(w, cookie.LoginLegacy, opts.WithSameSite(http.SameSiteDefaultMode))
}
