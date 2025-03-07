package session

import (
	"encoding"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/nais/wonderwall/pkg/crypto"
	"github.com/nais/wonderwall/pkg/openid"
)

const (
	RefreshMinInterval = 1 * time.Minute
	RefreshLeeway      = 5 * time.Minute
)

type EncryptedData struct {
	Data string `json:"data"`
}

var _ encoding.BinaryMarshaler = &EncryptedData{}
var _ encoding.BinaryUnmarshaler = &EncryptedData{}

func (in *EncryptedData) MarshalBinary() ([]byte, error) {
	return json.Marshal(in)
}

func (in *EncryptedData) UnmarshalBinary(bytes []byte) error {
	return json.Unmarshal(bytes, in)
}

func (in *EncryptedData) Decrypt(crypter crypto.Crypter) (*Data, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(in.Data)
	if err != nil {
		return nil, err
	}

	rawData, err := crypter.Decrypt(ciphertext)
	if err != nil {
		return nil, err
	}

	var data Data
	err = json.Unmarshal(rawData, &data)
	if err != nil {
		return nil, err
	}

	return &data, nil
}

type Data struct {
	ExternalSessionID string   `json:"external_session_id"`
	AccessToken       string   `json:"access_token"`
	IDToken           string   `json:"id_token"`
	RefreshToken      string   `json:"refresh_token"`
	IDTokenJwtID      string   `json:"id_token_jwt_id"`
	Metadata          Metadata `json:"metadata"`
}

func NewData(externalSessionID string, tokens *openid.Tokens, metadata *Metadata) *Data {
	data := &Data{
		ExternalSessionID: externalSessionID,
		AccessToken:       tokens.AccessToken,
		IDToken:           tokens.IDToken.GetSerialized(),
		IDTokenJwtID:      tokens.IDToken.GetJwtID(),
		RefreshToken:      tokens.RefreshToken,
	}

	if metadata != nil {
		data.Metadata = *metadata
	}

	return data
}

func (in *Data) Encrypt(crypter crypto.Crypter) (*EncryptedData, error) {
	bytes, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}

	ciphertext, err := crypter.Encrypt(bytes)
	if err != nil {
		return nil, err
	}

	return &EncryptedData{
		Data: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func (in *Data) HasAccessToken() bool {
	return len(in.AccessToken) > 0
}

func (in *Data) HasRefreshToken() bool {
	return len(in.RefreshToken) > 0
}

type Metadata struct {
	Session MetadataSession `json:"session"`
	Tokens  MetadataTokens  `json:"tokens"`
}

type MetadataSession struct {
	// CreatedAt is the time when the session was created.
	CreatedAt time.Time `json:"created_at"`
	// EndsAt is the time when the session will end, i.e. the absolute lifetime/time-to-live for the session.
	EndsAt time.Time `json:"ends_at"`
	// TimeoutAt is the time when the session will be marked as inactive. A zero value means no timeout. The timeout is extended whenever the tokens are refreshed.
	TimeoutAt time.Time `json:"timeout_at"`
}

type MetadataTokens struct {
	// ExpireAt is the time when the tokens will expire.
	ExpireAt time.Time `json:"expire_at"`
	// RefreshedAt is the time when the tokens were last refreshed.
	RefreshedAt time.Time `json:"refreshed_at"`
}

func NewMetadata(expiresIn, endsIn time.Duration) *Metadata {
	now := time.Now()
	return &Metadata{
		Session: MetadataSession{
			CreatedAt: now,
			EndsAt:    now.Add(endsIn),
		},
		Tokens: MetadataTokens{
			ExpireAt:    now.Add(expiresIn),
			RefreshedAt: now,
		},
	}
}

func (in *Metadata) IsExpired() bool {
	return time.Now().After(in.Tokens.ExpireAt)
}

func (in *Metadata) IsRefreshOnCooldown() bool {
	return time.Now().Before(in.RefreshCooldown())
}

func (in *Metadata) NextRefresh() time.Time {
	// subtract the leeway to ensure that we refresh before expiry
	next := in.Tokens.ExpireAt.Add(-RefreshLeeway)

	// try to refresh at the first opportunity if the next refresh is in the past
	if next.Before(time.Now()) {
		return in.RefreshCooldown()
	}

	return next
}

func (in *Metadata) Refresh(nextExpirySeconds int64) {
	now := time.Now()
	in.Tokens.RefreshedAt = now
	in.Tokens.ExpireAt = now.Add(time.Duration(nextExpirySeconds) * time.Second)
}

func (in *Metadata) RefreshCooldown() time.Time {
	refreshed := in.Tokens.RefreshedAt
	tokenLifetime := in.TokenLifetime()

	// if token lifetime is less than the minimum refresh interval * 2, we'll allow refreshes at the token half-life
	if tokenLifetime <= RefreshMinInterval*2 {
		return refreshed.Add(tokenLifetime / 2)
	}

	return refreshed.Add(RefreshMinInterval)
}

func (in *Metadata) ShouldRefresh() bool {
	if in.IsRefreshOnCooldown() {
		return false
	}

	return time.Now().After(in.NextRefresh())
}

func (in *Metadata) TokenLifetime() time.Duration {
	return in.Tokens.ExpireAt.Sub(in.Tokens.RefreshedAt)
}

func (in *Metadata) ExtendTimeout(duration time.Duration) {
	in.Session.TimeoutAt = time.Now().Add(duration)
}

func (in *Metadata) IsTimedOut() bool {
	if in.Session.TimeoutAt.IsZero() {
		return false
	}

	return time.Now().After(in.Session.TimeoutAt)
}

func (in *Metadata) WithTimeout(timeoutIn time.Duration) {
	in.Session.TimeoutAt = time.Now().Add(timeoutIn)
}

func (in *Metadata) Verbose() MetadataVerbose {
	now := time.Now()

	expireTime := in.Tokens.ExpireAt
	endTime := in.Session.EndsAt
	timeoutTime := in.Session.TimeoutAt

	mv := MetadataVerbose{
		Session: MetadataSessionVerbose{
			MetadataSession:  in.Session,
			EndsInSeconds:    toSeconds(endTime.Sub(now)),
			Active:           !in.IsTimedOut(),
			TimeoutInSeconds: toSeconds(timeoutTime.Sub(now)),
		},
		Tokens: MetadataTokensVerbose{
			MetadataTokens:  in.Tokens,
			ExpireInSeconds: toSeconds(expireTime.Sub(now)),
		},
	}

	if timeoutTime.IsZero() {
		mv.Session.TimeoutInSeconds = int64(-1)
	}

	return mv
}

func (in *Metadata) VerboseWithRefresh() MetadataVerboseWithRefresh {
	now := time.Now()

	verbose := in.Verbose()
	nextRefreshTime := in.NextRefresh()

	return MetadataVerboseWithRefresh{
		Session: verbose.Session,
		Tokens: MetadataTokensVerboseWithRefresh{
			MetadataTokensVerbose:    verbose.Tokens,
			NextAutoRefreshInSeconds: toSeconds(nextRefreshTime.Sub(now)),
			RefreshCooldown:          in.IsRefreshOnCooldown(),
			RefreshCooldownSeconds:   toSeconds(in.RefreshCooldown().Sub(now)),
		},
	}
}

type MetadataVerbose struct {
	Session MetadataSessionVerbose `json:"session"`
	Tokens  MetadataTokensVerbose  `json:"tokens"`
}

type MetadataVerboseWithRefresh struct {
	Session MetadataSessionVerbose           `json:"session"`
	Tokens  MetadataTokensVerboseWithRefresh `json:"tokens"`
}

type MetadataSessionVerbose struct {
	MetadataSession
	EndsInSeconds    int64 `json:"ends_in_seconds"`
	Active           bool  `json:"active"`
	TimeoutInSeconds int64 `json:"timeout_in_seconds"`
}

type MetadataTokensVerbose struct {
	MetadataTokens
	ExpireInSeconds int64 `json:"expire_in_seconds"`
}

type MetadataTokensVerboseWithRefresh struct {
	MetadataTokensVerbose
	NextAutoRefreshInSeconds int64 `json:"next_auto_refresh_in_seconds"`
	RefreshCooldown          bool  `json:"refresh_cooldown"`
	RefreshCooldownSeconds   int64 `json:"refresh_cooldown_seconds"`
}

func toSeconds(d time.Duration) int64 {
	i := int64(d.Seconds())
	if i <= 0 {
		return 0
	}

	return i
}
