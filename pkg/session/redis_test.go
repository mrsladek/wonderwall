package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	jwtlib "github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/nais/liberator/pkg/keygen"
	"github.com/stretchr/testify/assert"

	"github.com/nais/wonderwall/pkg/crypto"
	"github.com/nais/wonderwall/pkg/jwt"
	"github.com/nais/wonderwall/pkg/session"
)

func TestRedis(t *testing.T) {
	key, err := keygen.Keygen(32)
	assert.NoError(t, err)
	crypter := crypto.NewCrypter(key)

	idToken := jwtlib.New()
	idToken.Set("jti", "id-token-jti")

	accessToken := jwtlib.New()
	accessToken.Set("jti", "access-token-jti")

	tokens := &jwt.Tokens{
		IDToken:     jwt.NewIDToken("id_token", idToken),
		AccessToken: jwt.NewAccessToken("access_token", accessToken),
	}
	refreshToken := "some-refresh-token"

	data := session.NewData("myid", tokens, refreshToken)

	encryptedData, err := data.Encrypt(crypter)
	assert.NoError(t, err)

	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	defer s.Close()

	client := redis.NewClient(&redis.Options{
		Network: "tcp",
		Addr:    s.Addr(),
	})

	sess := session.NewRedis(client)
	err = sess.Write(context.Background(), "key", encryptedData, time.Minute)
	assert.NoError(t, err)

	result, err := sess.Read(context.Background(), "key")
	assert.NoError(t, err)
	assert.Equal(t, encryptedData, result)

	decrypted, err := result.Decrypt(crypter)
	assert.NoError(t, err)
	assert.Equal(t, data, decrypted)

	err = sess.Delete(context.Background(), "key")

	result, err = sess.Read(context.Background(), "key")
	assert.Error(t, err)
	assert.Nil(t, result)
}
