package main

import (
	"context"
	"fmt"

	"github.com/nais/liberator/pkg/conftools"
	log "github.com/sirupsen/logrus"

	"github.com/nais/wonderwall/pkg/config"
	"github.com/nais/wonderwall/pkg/crypto"
	"github.com/nais/wonderwall/pkg/logging"
	"github.com/nais/wonderwall/pkg/metrics"
	"github.com/nais/wonderwall/pkg/openid"
	"github.com/nais/wonderwall/pkg/router"
	"github.com/nais/wonderwall/pkg/server"
	"github.com/nais/wonderwall/pkg/session"
)

var maskedConfig = []string{
	config.OpenIDClientJWK,
	config.EncryptionKey,
	config.RedisPassword,
}

func run() error {
	cfg, err := config.Initialize()
	if err != nil {
		return err
	}
	if err := conftools.Load(cfg); err != nil {
		return err
	}

	if err := logging.Setup(cfg.LogLevel, cfg.LogFormat); err != nil {
		return err
	}

	log.Tracef("Trace logging enabled")

	for _, line := range conftools.Format(maskedConfig) {
		log.Info(line)
	}

	key, err := crypto.EncryptionKeyOrGenerate(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider, err := openid.NewProvider(ctx, cfg)
	if err != nil {
		return err
	}

	crypt := crypto.NewCrypter(key)
	sessionStore := session.NewStore(cfg)
	httplogger := logging.NewHttpLogger(cfg)
	h, err := router.NewHandler(*cfg, crypt, httplogger, provider, sessionStore)
	if err != nil {
		return fmt.Errorf("initializing routing handler: %w", err)
	}

	r := router.New(h)

	go func() {
		err := metrics.Handle(cfg.MetricsBindAddress)
		if err != nil {
			log.Fatalf("fatal: metrics server error: %s", err)
		}
	}()
	return server.Start(cfg, r)
}

func main() {
	err := run()
	if err != nil {
		log.Fatalf("Fatal error: %s", err)
	}
}
