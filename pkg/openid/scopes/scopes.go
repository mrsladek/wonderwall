package scopes

import (
	"fmt"
	"strings"
)

const (
	OpenID           = "openid"
	OfflineAccess    = "offline_access"
	AzureAPITemplate = "api://%s/.default"
)

type Scopes []string

func (s Scopes) String() string {
	return strings.Join(s, " ")
}

func (s Scopes) WithAdditional(scopes ...string) Scopes {
	return append(s, scopes...)
}

func (s Scopes) WithAzureScope(clientID string) Scopes {
	return append(s, fmt.Sprintf(AzureAPITemplate, clientID))
}

func (s Scopes) WithOfflineAccess() Scopes {
	return append(s, OfflineAccess)
}

func DefaultScopes() Scopes {
	return []string{OpenID}
}
