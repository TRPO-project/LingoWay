package database

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"lingoway/internal/models/llm"
	"lingoway/internal/models/topics"
	"lingoway/internal/models/users"

	"gorm.io/gorm"
)

// Дополнительные возможности PostgreSQL вынесены из GORM-тегов: составные FK,
// частичные/функциональные индексы и триггеры нельзя полноценно описать тегами.
// При изменении правил синхронизируйте этот файл с docs/schema.sql.
//
//go:embed invariants.sql
var invariantsSQL string

// migrationLock — постоянный ключ проекта: несколько экземпляров сервиса
// не должны одновременно менять одну и ту же схему.
const migrationLock int64 = 0x4c494e474f574159

// Migrate создаёт и обновляет общую схему public без удаления таблиц/данных.
// Вызывается до запуска HTTP-сервера, если DB_AUTO_MIGRATE=true.
// Переименования, удаление полей и преобразование старых данных требуют отдельной
// SQL-миграции: AutoMigrate не может определить намерение разработчика.
func Migrate(ctx context.Context, db *gorm.DB) error {
	if db == nil || db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return errors.New("database migrations require a PostgreSQL connection")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", migrationLock).Error; err != nil {
			return fmt.Errorf("lock database migration: %w", err)
		}
		// Схема проекта всегда public, независимо от search_path в DSN.
		if err := tx.Exec("SET LOCAL search_path TO public").Error; err != nil {
			return fmt.Errorf("set migration schema: %w", err)
		}
		if err := tx.AutoMigrate(
			&users.User{}, &topics.Topic{}, &llm.Material{}, &llm.Module{},
			&llm.Exercise{}, &llm.Answer{}, &llm.LLMRequest{},
		); err != nil {
			return fmt.Errorf("auto-migrate models: %w", err)
		}
		// Явно поддерживаем обновление предыдущей схемы с обязательным автором.
		// Существующие владельцы сохраняются; NULL разрешён для системного каталога.
		if err := tx.Exec("ALTER TABLE public.modules ALTER COLUMN created_by_user_id DROP NOT NULL").Error; err != nil {
			return fmt.Errorf("make module creator optional: %w", err)
		}
		if err := migrateCorrectnessColumn(tx); err != nil {
			return err
		}
		// Каждый блок — одно выражение PostgreSQL. Нельзя делить SQL по ';':
		// внутри PL/pgSQL-функций и DO-блоков тоже есть точки с запятой.
		for index, statement := range strings.Split(invariantsSQL, "-- migrate:statement") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("apply database invariant %d: %w", index+1, err)
			}
		}
		return nil
	})
}

func migrateCorrectnessColumn(tx *gorm.DB) error {
	// GORM видит IsCorrect только при чтении и не пытается записать/изменить его.
	if err := tx.Exec(`ALTER TABLE public.answers ADD COLUMN IF NOT EXISTS
		is_correct boolean GENERATED ALWAYS AS (score = 1) STORED`).Error; err != nil {
		return fmt.Errorf("create answer correctness column: %w", err)
	}
	var valid bool
	if err := tx.Raw(`SELECT a.attgenerated = 's' AND a.atttypid = 'boolean'::regtype
		AND regexp_replace(pg_get_expr(d.adbin, d.adrelid), '[[:space:]()]', '', 'g')
			IN ('score=1::numeric', 'score=1')
		FROM pg_attribute a
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = 'public.answers'::regclass AND a.attname = 'is_correct'
		AND NOT a.attisdropped`).Scan(&valid).Error; err != nil {
		return fmt.Errorf("inspect answer correctness column: %w", err)
	}
	if !valid {
		return errors.New("answers.is_correct must be a stored generated boolean (score = 1); an explicit SQL migration is required")
	}
	return nil
}
