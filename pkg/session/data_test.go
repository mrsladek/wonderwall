package session_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nais/wonderwall/pkg/session"
)

func TestData_HasAccessToken(t *testing.T) {
	data := session.Data{}
	assert.False(t, data.HasAccessToken())

	data.AccessToken = "some-access-token"
	assert.True(t, data.HasAccessToken())
}

func TestData_HasRefreshToken(t *testing.T) {
	data := session.Data{}
	assert.False(t, data.HasRefreshToken())

	data.RefreshToken = "some-refresh-token"
	assert.True(t, data.HasRefreshToken())
}

func TestMetadata_IsExpired(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				ExpireAt: time.Now(),
			},
		}

		assert.True(t, metadata.IsExpired())
	})

	t.Run("not expired", func(t *testing.T) {
		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				ExpireAt: time.Now().Add(time.Second),
			},
		}

		assert.False(t, metadata.IsExpired())
	})
}

func TestMetadata_IsRefreshOnCooldown(t *testing.T) {
	t.Run("delta to last refresh below minimum interval", func(t *testing.T) {
		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				RefreshedAt: time.Now(),
				ExpireAt:    time.Now().Add(time.Minute),
			},
		}

		assert.True(t, metadata.IsRefreshOnCooldown())
	})

	t.Run("delta to last refresh above minimum interval", func(t *testing.T) {
		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				RefreshedAt: time.Now().Add(-2 * time.Minute),
				ExpireAt:    time.Now().Add(time.Minute),
			},
		}

		assert.False(t, metadata.IsRefreshOnCooldown())
	})
}

func TestMetadata_NextRefresh(t *testing.T) {
	t.Run("delta to last refresh below minimum interval", func(t *testing.T) {
		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				RefreshedAt: time.Now(),
				ExpireAt:    time.Now().Add(time.Minute),
			},
		}

		assert.True(t, metadata.IsRefreshOnCooldown())
	})

	t.Run("delta to last refresh above minimum interval", func(t *testing.T) {
		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				RefreshedAt: time.Now().Add(-2 * time.Minute),
				ExpireAt:    time.Now().Add(time.Minute),
			},
		}

		assert.False(t, metadata.IsRefreshOnCooldown())
	})
}

func TestMetadata_Refresh(t *testing.T) {
	metadata := session.Metadata{
		Tokens: session.MetadataTokens{
			RefreshedAt: time.Now(),
			ExpireAt:    time.Now().Add(time.Minute),
		},
	}

	prevRefreshedAt := metadata.Tokens.RefreshedAt
	prevExpireAt := metadata.Tokens.ExpireAt

	nextExpirySeconds := int64((2 * time.Minute).Seconds())
	metadata.Refresh(nextExpirySeconds)

	assert.True(t, metadata.Tokens.RefreshedAt.After(prevRefreshedAt))
	assert.True(t, metadata.Tokens.ExpireAt.After(prevExpireAt))
}

func TestMetadata_RefreshCooldown(t *testing.T) {
	t.Run("token lifetime less than interval", func(t *testing.T) {
		tokenLifetime := time.Minute

		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				RefreshedAt: time.Now(),
				ExpireAt:    time.Now().Add(tokenLifetime),
			},
		}

		expected := time.Now().Add(tokenLifetime / 2)
		assert.WithinDuration(t, expected, metadata.RefreshCooldown(), time.Second)
	})

	t.Run("token lifetime longer than interval", func(t *testing.T) {
		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				RefreshedAt: time.Now(),
				ExpireAt:    time.Now().Add(time.Hour),
			},
		}

		expected := metadata.Tokens.RefreshedAt.Add(session.RefreshMinInterval)
		assert.WithinDuration(t, expected, metadata.RefreshCooldown(), time.Second)
	})
}

func TestMetadata_ShouldRefresh(t *testing.T) {
	t.Run("refresh is on cooldown", func(t *testing.T) {
		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				RefreshedAt: time.Now(),
				ExpireAt:    time.Now().Add(time.Minute),
			},
		}

		assert.False(t, metadata.ShouldRefresh())
	})

	t.Run("token is not within expiry range", func(t *testing.T) {
		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				RefreshedAt: time.Now(),
				ExpireAt:    time.Now().Add(time.Hour),
			},
		}

		assert.False(t, metadata.ShouldRefresh())
	})

	t.Run("token is about to expire", func(t *testing.T) {
		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				RefreshedAt: time.Now().Add(-5 * time.Minute),
				ExpireAt:    time.Now().Add(time.Minute),
			},
		}

		assert.True(t, metadata.ShouldRefresh())
	})

	t.Run("token has expired", func(t *testing.T) {
		metadata := session.Metadata{
			Tokens: session.MetadataTokens{
				RefreshedAt: time.Now().Add(-5 * time.Minute),
				ExpireAt:    time.Now().Add(-5 * time.Minute),
			},
		}

		assert.True(t, metadata.ShouldRefresh())
	})
}

func TestMetadata_TokenLifetime(t *testing.T) {
	metadata := session.Metadata{
		Tokens: session.MetadataTokens{
			RefreshedAt: time.Now(),
			ExpireAt:    time.Now().Add(time.Minute),
		},
	}

	assert.Equal(t, time.Minute, metadata.TokenLifetime().Truncate(time.Second))
}

func TestMetadata_Verbose(t *testing.T) {
	tokenLifetime := 30 * time.Minute
	sessionLifetime := time.Hour

	metadata := session.NewMetadata(tokenLifetime, sessionLifetime)

	verbose := metadata.Verbose()
	maxDelta := time.Second

	expected := time.Now().Add(sessionLifetime)
	actual := time.Now().Add(durationSeconds(verbose.Session.EndsInSeconds))
	assert.WithinDuration(t, expected, actual, maxDelta)

	expected = time.Now().Add(tokenLifetime)
	actual = time.Now().Add(durationSeconds(verbose.Tokens.ExpireInSeconds))
	assert.WithinDuration(t, expected, actual, maxDelta)

	expected = time.Now().Add(tokenLifetime).Add(-session.RefreshLeeway)
	actual = time.Now().Add(durationSeconds(verbose.Tokens.NextAutoRefreshInSeconds))
	assert.WithinDuration(t, expected, actual, maxDelta)

	t.Run("refresh on cooldown", func(t *testing.T) {
		assert.True(t, verbose.Tokens.RefreshCooldown)

		expected = time.Now().Add(session.RefreshMinInterval)
		actual = time.Now().Add(durationSeconds(verbose.Tokens.RefreshCooldownSeconds))
		assert.WithinDuration(t, expected, actual, maxDelta)
	})

	t.Run("refresh not on cooldown", func(t *testing.T) {
		metadata := session.NewMetadata(tokenLifetime, sessionLifetime)
		metadata.Tokens.RefreshedAt = time.Now().Add(-5 * time.Minute)
		verbose := metadata.Verbose()

		assert.False(t, verbose.Tokens.RefreshCooldown)
		assert.Equal(t, int64(0), verbose.Tokens.RefreshCooldownSeconds)
	})
}

func durationSeconds(seconds int64) time.Duration {
	return time.Duration(seconds) * time.Second
}
