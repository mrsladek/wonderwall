package router

import (
	"net/http"

	log "github.com/sirupsen/logrus"
)

// FrontChannelLogout triggers logout triggered by a third-party.
func (h *Handler) FrontChannelLogout(w http.ResponseWriter, r *http.Request) {
	// Unconditionally return HTTP 200 OK
	w.WriteHeader(http.StatusOK)

	params := r.URL.Query()
	sid := params.Get("sid")

	// Unconditionally destroy all local references to the session.
	h.deleteCookie(w, h.GetSessionCookieName())

	if len(sid) == 0 {
		log.Info("sid parameter not set in request; ignoring")
		h.DeleteSessionFallback(w, r)
		return
	}

	sessionID := h.localSessionID(sid)

	err := h.destroySession(w, r, sessionID)
	if err != nil {
		log.Error(err)
		// Session is already destroyed at the OP and is highly unlikely to be used again.
	}
}
