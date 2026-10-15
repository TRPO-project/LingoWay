package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"lingoway/internal/config"
)

func connectionTestConfig() config.Config {
	return config.Config{
		DatabaseURL: "postgres://test:must-not-leak@127.0.0.1:1/lingoway_test?sslmode=disable",
		Database: config.DatabaseConfig{
			MaxOpenConns: 2, MaxIdleConns: 1,
			ConnectTimeout: time.Second, ConnMaxLifetime: time.Minute,
		},
	}
}

func TestOpenDoesNotLeakConnectionStringOnParseFailure(t *testing.T) {
	cfg := connectionTestConfig()
	cfg.DatabaseURL = "postgres://test:must-not-leak@%invalid/lingoway_test"
	db, err := Open(context.Background(), cfg)
	if err == nil || db != nil {
		t.Fatalf("invalid PostgreSQL URL accepted: db=%v, err=%v", db, err)
	}
	if strings.Contains(err.Error(), "must-not-leak") || strings.Contains(err.Error(), cfg.DatabaseURL) {
		t.Fatal("connection error exposed database credentials")
	}
}

func TestOpenHonorsCancelledStartupContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db, err := Open(ctx, connectionTestConfig())
	if db != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled connection startup: db=%v, err=%v", db, err)
	}
	if strings.Contains(err.Error(), "must-not-leak") {
		t.Fatal("connection error exposed database credentials")
	}
}

func TestOpenRejectsUnlimitedOrInvalidPool(t *testing.T) {
	for _, change := range []func(*config.Config){
		func(cfg *config.Config) { cfg.DatabaseURL = "" },
		func(cfg *config.Config) { cfg.Database.MaxOpenConns = 0 },
		func(cfg *config.Config) { cfg.Database.MaxIdleConns = -1 },
		func(cfg *config.Config) { cfg.Database.MaxIdleConns = cfg.Database.MaxOpenConns + 1 },
		func(cfg *config.Config) { cfg.Database.ConnectTimeout = 0 },
		func(cfg *config.Config) { cfg.Database.ConnMaxLifetime = 0 },
	} {
		cfg := connectionTestConfig()
		change(&cfg)
		if db, err := Open(context.Background(), cfg); err == nil || db != nil {
			t.Fatal("invalid connection configuration accepted")
		}
	}
}
