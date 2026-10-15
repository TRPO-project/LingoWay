package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func isolatedEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"PORT", "DATABASE_URL", "JWT_SECRET", "DB_AUTO_MIGRATE", "DB_MAX_OPEN_CONNS",
		"DB_MAX_IDLE_CONNS", "DB_CONN_MAX_LIFETIME", "DB_CONNECT_TIMEOUT", "DB_MIGRATION_TIMEOUT",
	} {
		// t.Setenv зарегистрирует восстановление исходного значения после теста.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DATABASE_URL", "postgres://test:example@localhost:5432/lingoway_test")
}

func TestDatabaseConfigurationDefaults(t *testing.T) {
	isolatedEnvironment(t)
	cfg, err := fromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	want := DatabaseConfig{
		AutoMigrate: true, MaxOpenConns: 10, MaxIdleConns: 5,
		ConnMaxLifetime: 30 * time.Minute, ConnectTimeout: 5 * time.Second,
		MigrationTimeout: time.Minute,
	}
	if cfg.Database != want || cfg.Port != "8080" {
		t.Fatalf("unexpected defaults: database=%+v, port=%q", cfg.Database, cfg.Port)
	}
}

func TestDatabaseConfigurationOverrides(t *testing.T) {
	isolatedEnvironment(t)
	for key, value := range map[string]string{
		"PORT": "9090", "DB_AUTO_MIGRATE": "false", "DB_MAX_OPEN_CONNS": "20",
		"DB_MAX_IDLE_CONNS": "0", "DB_CONN_MAX_LIFETIME": "15m",
		"DB_CONNECT_TIMEOUT": "2s", "DB_MIGRATION_TIMEOUT": "90s",
	} {
		t.Setenv(key, value)
	}
	cfg, err := fromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	want := DatabaseConfig{
		AutoMigrate: false, MaxOpenConns: 20, MaxIdleConns: 0,
		ConnMaxLifetime: 15 * time.Minute, ConnectTimeout: 2 * time.Second,
		MigrationTimeout: 90 * time.Second,
	}
	if cfg.Database != want || cfg.Port != "9090" {
		t.Fatalf("overrides not applied: database=%+v, port=%q", cfg.Database, cfg.Port)
	}
}

func TestInvalidConfigurationFailsBeforeDatabaseStartup(t *testing.T) {
	cases := []struct {
		key, value string
	}{
		{"DATABASE_URL", " "},
		{"PORT", "invalid"}, {"PORT", "0"}, {"PORT", "65536"},
		{"DB_AUTO_MIGRATE", "sometimes"}, {"DB_AUTO_MIGRATE", ""},
		{"DB_MAX_OPEN_CONNS", "0"}, {"DB_MAX_OPEN_CONNS", "invalid"},
		{"DB_MAX_IDLE_CONNS", "-1"}, {"DB_MAX_IDLE_CONNS", "11"},
		{"DB_CONN_MAX_LIFETIME", "0s"}, {"DB_CONN_MAX_LIFETIME", "invalid"},
		{"DB_CONNECT_TIMEOUT", "-1s"}, {"DB_CONNECT_TIMEOUT", "5"},
		{"DB_MIGRATION_TIMEOUT", "0s"}, {"DB_MIGRATION_TIMEOUT", ""},
	}
	for _, tc := range cases {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			isolatedEnvironment(t)
			t.Setenv(tc.key, tc.value)
			_, err := fromEnvironment()
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected actionable error mentioning %s, got %v", tc.key, err)
			}
		})
	}
}
