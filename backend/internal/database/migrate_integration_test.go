package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lingoway/internal/config"
	"lingoway/internal/models/llm"
	"lingoway/internal/models/topics"
	"lingoway/internal/models/users"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Интеграционные проверки запускаются только явно: TEST_DATABASE_URL должен
// указывать на служебную БД PostgreSQL с правом CREATE DATABASE. Каждый сценарий
// создаёт отдельную случайную базу и удаляет только её; исходная БД не изменяется.
func isolatedPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("set TEST_DATABASE_URL to run isolated PostgreSQL integration tests")
	}
	databaseURL, err := url.Parse(adminURL)
	if err != nil || (databaseURL.Scheme != "postgres" && databaseURL.Scheme != "postgresql") {
		t.Fatal("TEST_DATABASE_URL must use postgres:// or postgresql://")
	}
	adminConfig, err := pgx.ParseConfig(adminURL)
	if err != nil {
		t.Fatal("TEST_DATABASE_URL is not a valid PostgreSQL connection string")
	}
	adminConfig.ConnectTimeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("cannot connect to integration PostgreSQL administration database")
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		_ = admin.Close(context.Background())
		t.Fatal(err)
	}
	name := "lingoway_gorm_test_" + hex.EncodeToString(suffix[:])
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		_ = admin.Close(context.Background())
		t.Fatalf("create isolated integration database: %v", err)
	}
	var db *gorm.DB
	t.Cleanup(func() {
		if db != nil {
			if pool, err := db.DB(); err == nil {
				_ = pool.Close()
			}
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// identifier создан выше из фиксированного префикса и случайного hex;
		// никакое имя из TEST_DATABASE_URL здесь не используется для удаления.
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE "+identifier+" WITH (FORCE)"); err != nil {
			t.Errorf("remove isolated integration database %s: %v", name, err)
		}
		_ = admin.Close(cleanupCtx)
	})
	// pgx.ConnConfig.ConnString() возвращает исходную строку даже после замены
	// Config.Database, поэтому для изоляции меняем именно URL, а не копию Config.
	databaseURL.Path = "/" + name
	databaseURL.RawPath = ""
	query := databaseURL.Query()
	query.Del("dbname")
	query.Del("database")
	query.Set("search_path", "other") // Open обязан закрепить схему public.
	databaseURL.RawQuery = query.Encode()
	cfg := config.Config{
		DatabaseURL: databaseURL.String(),
		Database: config.DatabaseConfig{
			MaxOpenConns: 5, MaxIdleConns: 2, ConnMaxLifetime: time.Minute,
			ConnectTimeout: 5 * time.Second, MigrationTimeout: time.Minute,
		},
	}
	db, err = Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	var actualDatabase, searchPath string
	if err := db.Raw("SELECT current_database()").Scan(&actualDatabase).Error; err != nil || actualDatabase != name {
		t.Fatal("integration connection did not select the newly created isolated database")
	}
	if err := db.Raw("SHOW search_path").Scan(&searchPath).Error; err != nil || searchPath != "public" {
		t.Fatalf("runtime search_path must be public: %q, %v", searchPath, err)
	}
	return db
}

func TestPostgresMigrateExistingAndEmptySchemas(t *testing.T) {
	for _, mode := range []string{"empty", "sql_bootstrap", "legacy_required_creator"} {
		t.Run(mode, func(t *testing.T) {
			db := isolatedPostgres(t)
			var existingUserID int64
			if mode != "empty" {
				bootstrap, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "schema.sql"))
				if err != nil {
					t.Fatal(err)
				}
				mustExec(t, db, string(bootstrap))
				if mode == "legacy_required_creator" {
					mustExec(t, db, "ALTER TABLE public.modules ALTER COLUMN created_by_user_id SET NOT NULL")
				}
				user := users.User{Email: "existing@example.test", PasswordHash: "test-hash", Name: "Existing user"}
				mustCreate(t, db, &user)
				existingUserID = user.ID
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if err := Migrate(ctx, db); err != nil {
				t.Fatalf("first migration: %v", err)
			}
			before := publicConstraintCount(t, db)
			if err := Migrate(ctx, db); err != nil {
				t.Fatalf("repeat migration: %v", err)
			}
			if after := publicConstraintCount(t, db); after != before {
				t.Fatalf("repeat migration changed constraint count: %d -> %d", before, after)
			}
			var tableCount int64
			if err := db.Raw("SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'").Scan(&tableCount).Error; err != nil || tableCount != 7 {
				t.Fatalf("expected exactly seven entity tables: count=%d, err=%v", tableCount, err)
			}
			var nullable string
			if err := db.Raw("SELECT is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'modules' AND column_name = 'created_by_user_id'").Scan(&nullable).Error; err != nil || nullable != "YES" {
				t.Fatalf("module creator must be nullable: %q, %v", nullable, err)
			}
			if existingUserID != 0 {
				var user users.User
				if err := db.First(&user, existingUserID).Error; err != nil || user.Email != "existing@example.test" {
					t.Fatalf("existing data was not preserved: %+v, %v", user, err)
				}
			}
			exerciseEntitiesAndInvariants(t, db)
		})
	}
}

func TestPostgresConcurrentMigrations(t *testing.T) {
	db := isolatedPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	start := make(chan struct{})
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errors <- Migrate(ctx, db)
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent migration failed: %v", err)
		}
	}
	before := publicConstraintCount(t, db)
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if after := publicConstraintCount(t, db); after != before {
		t.Fatalf("concurrent startup produced unstable constraints: %d -> %d", before, after)
	}
}

func TestPostgresModelUpdateRestoresMissingColumnAndPreservesData(t *testing.T) {
	db := isolatedPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	owner := users.User{Email: "upgrade@example.test", PasswordHash: "test-hash", Name: "Upgrade fixture"}
	mustCreate(t, db, &owner)
	source := "A text that must survive a schema update."
	material := llm.Material{OwnerUserID: owner.ID, Title: "Existing source", Kind: llm.MaterialText, SourceText: &source}
	mustCreate(t, db, &material)
	request := llm.LLMRequest{OwnerUserID: owner.ID, Operation: llm.LLMPrepareMaterial, MaterialID: &material.ID, PromptVersion: "v1"}
	mustCreate(t, db, &request)
	// Только в созданной этим тестом базе имитируем предыдущую версию модели,
	// у которой ещё нет необязательного столбца без связанных CHECK/индексов.
	mustExec(t, db, "ALTER TABLE public.llm_requests DROP COLUMN error_message")
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("update existing populated schema: %v", err)
	}
	var columnExists bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'llm_requests' AND column_name = 'error_message')`).Scan(&columnExists).Error; err != nil || !columnExists {
		t.Fatalf("AutoMigrate did not restore the model column: exists=%v, err=%v", columnExists, err)
	}
	var loaded llm.LLMRequest
	if err := db.First(&loaded, request.ID).Error; err != nil || loaded.RequestKey != request.RequestKey || loaded.OwnerUserID != owner.ID || loaded.Status != llm.LLMQueued || loaded.ErrorMessage != nil {
		t.Fatalf("schema update did not preserve the queued request: %+v, %v", loaded, err)
	}
	var loadedMaterial llm.Material
	if err := db.First(&loadedMaterial, material.ID).Error; err != nil || loadedMaterial.SourceText == nil || *loadedMaterial.SourceText != source {
		t.Fatalf("schema update did not preserve the source material: %+v, %v", loadedMaterial, err)
	}
}

func TestPostgresMigrationRollsBackIncompatibleGeneratedColumn(t *testing.T) {
	db := isolatedPostgres(t)
	mustExec(t, db, "CREATE TABLE public.answers (id bigserial PRIMARY KEY, is_correct boolean)")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := Migrate(ctx, db)
	if err == nil || !strings.Contains(err.Error(), "is_correct must be a stored generated boolean") {
		t.Fatalf("expected explicit generated-column incompatibility, got %v", err)
	}
	var usersExist bool
	if err := db.Raw("SELECT to_regclass('public.users') IS NOT NULL").Scan(&usersExist).Error; err != nil || usersExist {
		t.Fatalf("failed migration left a newly created users table: exists=%v, err=%v", usersExist, err)
	}
	var columns int64
	if err := db.Raw("SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'answers'").Scan(&columns).Error; err != nil || columns != 2 {
		t.Fatalf("failed migration changed the pre-existing answers table: columns=%d, err=%v", columns, err)
	}
}

func exerciseEntitiesAndInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	owner := users.User{Email: "student@example.test", PasswordHash: "test-hash", Name: "Student"}
	other := users.User{Email: "other@example.test", PasswordHash: "test-hash", Name: "Other"}
	mustCreate(t, db, &owner)
	mustCreate(t, db, &other)
	if owner.ID == 0 || owner.CreatedAt.IsZero() || owner.Role != users.RoleStudent {
		t.Fatalf("user database defaults not returned: %+v", owner)
	}
	mustExec(t, db.Model(&owner), "UPDATE public.users SET name = ? WHERE id = ?", "Renamed student", owner.ID)
	duplicateEmail := users.User{Email: " STUDENT@example.test ", PasswordHash: "test-hash", Name: "Duplicate"}
	wantSQLState(t, db.Create(&duplicateEmail).Error, "23505")

	topic := topics.Topic{Slug: "travel", Name: "Travel"}
	mustCreate(t, db, &topic)
	if err := db.Model(&topic).Update("name", "Travel and holidays").Error; err != nil {
		t.Fatal(err)
	}
	source := "A small text about travel."
	material := llm.Material{OwnerUserID: owner.ID, TopicID: &topic.ID, Title: "Input", Kind: llm.MaterialText, SourceText: &source}
	mustCreate(t, db, &material)
	if material.RequestKey == "" || material.CreatedAt.IsZero() || material.Status != llm.MaterialPending {
		t.Fatalf("material database defaults not returned: %+v", material)
	}
	if err := db.Model(&material).Update("title", "Renamed input").Error; err != nil {
		t.Fatal(err)
	}
	orphan := llm.Material{OwnerUserID: 999999999, Title: "Orphan", Kind: llm.MaterialText, SourceText: &source}
	wantSQLState(t, db.Create(&orphan).Error, "23503")

	newModule := func(ownerID *int64, materialID *int64, visibility llm.ModuleVisibility) llm.Module {
		return llm.Module{CreatedByUserID: ownerID, MaterialID: materialID, TopicID: topic.ID, Title: "Travel module", Difficulty: llm.LevelA1, Visibility: visibility}
	}
	ownerless := newModule(nil, nil, llm.VisibilityPrivate)
	wantSQLState(t, db.Create(&ownerless).Error, "23514")
	ownerlessSource := newModule(nil, &material.ID, llm.VisibilityPublic)
	wantSQLState(t, db.Create(&ownerlessSource).Error, "23514")
	wrongOwner := newModule(&other.ID, &material.ID, llm.VisibilityPrivate)
	wantSQLState(t, db.Create(&wrongOwner).Error, "23503")
	systemModule := newModule(nil, nil, llm.VisibilityPublic)
	mustCreate(t, db, &systemModule)
	privateModule := newModule(&owner.ID, &material.ID, llm.VisibilityPrivate)
	mustCreate(t, db, &privateModule)
	if err := db.Model(&privateModule).Update("title", "My travel module").Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	readyAtCreation := newModule(nil, nil, llm.VisibilityPublic)
	readyAtCreation.Status, readyAtCreation.PublishedAt = llm.ModuleReady, &now
	wantSQLState(t, db.Create(&readyAtCreation).Error, "P0001")

	makeExercise := func(moduleID int64, position int) llm.Exercise {
		return llm.Exercise{
			ModuleID: moduleID, Position: position, Type: llm.ExerciseVocabularyGap,
			Instruction: "Fill in the gap", QuestionText: "I like ____.",
			Content:   llm.JSON(`{"options":[{"id":"a","text":"travel"}]}`),
			AnswerKey: llm.JSON(`{"correct_option_id":"a"}`), Explanation: "Travel is the correct noun.",
		}
	}
	privateExercise := makeExercise(privateModule.ID, 1)
	secondPrivate := makeExercise(privateModule.ID, 2)
	systemExercise := makeExercise(systemModule.ID, 1)
	secondSystem := makeExercise(systemModule.ID, 2)
	for _, exercise := range []*llm.Exercise{&privateExercise, &secondPrivate, &systemExercise, &secondSystem} {
		mustCreate(t, db, exercise)
	}
	if err := db.Model(&privateExercise).Update("instruction", "Choose the word").Error; err != nil {
		t.Fatal(err)
	}
	var loadedExercise llm.Exercise
	if err := db.Preload("Module").First(&loadedExercise, privateExercise.ID).Error; err != nil || string(loadedExercise.Content) == "" || loadedExercise.Module == nil {
		t.Fatalf("exercise JSONB/association read failed: %+v, %v", loadedExercise, err)
	}
	// JSONB can reorder keys/whitespace, so compare the meaningful payload.
	if !strings.Contains(string(loadedExercise.Content), `"travel"`) || !strings.Contains(string(loadedExercise.AnswerKey), `"a"`) {
		t.Fatal("exercise JSONB did not round-trip")
	}
	for _, module := range []*llm.Module{&privateModule, &systemModule} {
		if err := db.Model(module).Updates(map[string]any{"status": llm.ModuleReady, "published_at": now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	wantSQLState(t, db.Model(&privateModule).Update("title", "Forbidden edit").Error, "P0001")
	lateExercise := makeExercise(privateModule.ID, 3)
	wantSQLState(t, db.Create(&lateExercise).Error, "P0001")
	wantSQLState(t, db.Model(&privateExercise).Update("instruction", "Forbidden edit").Error, "P0001")
	wantSQLState(t, db.Delete(&secondPrivate).Error, "P0001")

	incorrect := false
	forcedCorrect := true
	answer := llm.Answer{UserID: owner.ID, ExerciseID: privateExercise.ID, Response: llm.JSON(`{"text":"travel"}`), IsCorrect: &forcedCorrect}
	mustCreate(t, db, &answer)
	if err := db.First(&answer, answer.ID).Error; err != nil || answer.IsCorrect != nil || answer.Score != nil || answer.RequestKey == "" {
		t.Fatalf("pending answer defaults/generated NULL wrong: %+v, %v", answer, err)
	}
	if err := db.Model(&answer).Updates(map[string]any{
		"status": llm.AnswerChecked, "score": 1, "feedback": "Correct", "checked_at": now,
		"is_correct": incorrect, // read-only GORM field must never be written.
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&answer, answer.ID).Error; err != nil || answer.IsCorrect == nil || !*answer.IsCorrect || answer.Score == nil || *answer.Score != 1 {
		t.Fatalf("generated answer correctness wrong: %+v, %v", answer, err)
	}
	forbiddenAnswer := llm.Answer{UserID: other.ID, ExerciseID: privateExercise.ID, Response: llm.JSON(`{"text":"travel"}`)}
	wantSQLState(t, db.Create(&forbiddenAnswer).Error, "P0001")
	publicAnswer := llm.Answer{UserID: other.ID, ExerciseID: systemExercise.ID, Response: llm.JSON(`{"text":"travel"}`)}
	mustCreate(t, db, &publicAnswer)

	request := llm.LLMRequest{OwnerUserID: owner.ID, Operation: llm.LLMPrepareMaterial, MaterialID: &material.ID, PromptVersion: "v1"}
	mustCreate(t, db, &request)
	if request.RequestKey == "" || request.CreatedAt.IsZero() || request.AvailableAt.IsZero() || request.Status != llm.LLMQueued || string(request.Parameters) != "{}" {
		t.Fatalf("LLM database defaults not returned: %+v", request)
	}
	duplicateWork := llm.LLMRequest{OwnerUserID: owner.ID, Operation: llm.LLMPrepareMaterial, MaterialID: &material.ID, PromptVersion: "v1"}
	wantSQLState(t, db.Create(&duplicateWork).Error, "23505")
	lease := "6c04bf47-2294-41d2-84b1-4b1ab7e665bd"
	if err := db.Model(&request).Updates(map[string]any{
		"status": llm.LLMRunning, "lease_token": lease, "lease_expires_at": now.Add(time.Minute), "attempt_count": 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&request).Updates(map[string]any{
		"status": llm.LLMSucceeded, "lease_token": nil, "lease_expires_at": nil,
		"result": llm.JSON(`{"prepared":true}`), "finished_at": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&request, request.ID).Error; err != nil || request.Status != llm.LLMSucceeded || !strings.Contains(string(request.Result), "true") {
		t.Fatalf("LLM result JSONB did not round-trip: %+v, %v", request, err)
	}
	moduleRequest := llm.LLMRequest{OwnerUserID: owner.ID, Operation: llm.LLMGenerateModule, ModuleID: &privateModule.ID, PromptVersion: "v1"}
	answerRequest := llm.LLMRequest{OwnerUserID: owner.ID, Operation: llm.LLMCheckAnswer, AnswerID: &answer.ID, PromptVersion: "v1"}
	mustCreate(t, db, &moduleRequest)
	mustCreate(t, db, &answerRequest)

	// Удаление исходного материала удаляет производный личный модуль целиком,
	// задания, ответы и связанные LLM-запросы, включая опубликованный модуль.
	if err := db.Delete(&material).Error; err != nil {
		t.Fatalf("whole-material cascade failed: %v", err)
	}
	for _, row := range []struct {
		table string
		id    int64
	}{
		{"materials", material.ID}, {"modules", privateModule.ID}, {"exercises", privateExercise.ID},
		{"answers", answer.ID}, {"llm_requests", request.ID}, {"llm_requests", moduleRequest.ID}, {"llm_requests", answerRequest.ID},
	} {
		var count int64
		if err := db.Table(row.table).Where("id = ?", row.id).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("cascade left %s id=%d: count=%d, err=%v", row.table, row.id, count, err)
		}
	}
	for _, entity := range []any{&systemModule, &topic, &owner, &other} {
		if err := db.Delete(entity).Error; err != nil {
			t.Fatalf("entity cleanup failed for %T: %v", entity, err)
		}
	}
}

func mustCreate(t *testing.T, db *gorm.DB, value any) {
	t.Helper()
	if err := db.Create(value).Error; err != nil {
		t.Fatalf("create %T: %v", value, err)
	}
}

func mustExec(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatal(err)
	}
}

func publicConstraintCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT count(*) FROM pg_constraint WHERE connamespace = 'public'::regnamespace").Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func wantSQLState(t *testing.T, err error, state string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != state {
		t.Fatalf("expected PostgreSQL SQLSTATE %s, got %v", state, err)
	}
}
