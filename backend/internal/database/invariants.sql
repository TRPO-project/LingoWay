-- Дополнительные гарантии PostgreSQL для моделей GORM.
-- Источник структуры и имён: docs/schema.sql (общая схема v0.2).
-- Файл выполняется ПОСЛЕ AutoMigrate всех семи таблиц, в транзакции вызывающего Go-кода.
-- Сначала создаются составные UNIQUE, затем CHECK и FK, затем индексы и триггеры.
-- Сохраняем точные имена bootstrap: повторный запуск не дублирует его ограничения.
-- При изменении схемы синхронизируйте docs/schema.sql, модели и этот файл.
-- Уже существующие ограничения не заменяются: их изменение требует отдельной миграции.
-- PK, NOT NULL и уникальность topics.slug обслуживает GORM; вычисляемый
-- answers.is_correct создаёт Go-миграция. Здесь нет внешних BEGIN/COMMIT.
-- SQL использует возможности PostgreSQL 16; PG18 NOT NULL constraints не экспортируются.

-- Ordinal preserves UNIQUE -> CHECK -> FK dependency order.
-- Keep definitions as data so all 72 constraints share one idempotent installer.
DO $$
DECLARE
    constraint_record record;
BEGIN
    FOR constraint_record IN
        SELECT * FROM (VALUES
        (1, 'answers', 'answers_id_user_id_key',
            $ddl$UNIQUE (id, user_id)$ddl$),
        (2, 'answers', 'answers_user_id_request_key_key',
            $ddl$UNIQUE (user_id, request_key)$ddl$),
        (3, 'exercises', 'exercises_module_id_position_key',
            $ddl$UNIQUE (module_id, "position")$ddl$),
        (4, 'llm_requests', 'llm_requests_owner_user_id_operation_request_key_key',
            $ddl$UNIQUE (owner_user_id, operation, request_key)$ddl$),
        (5, 'materials', 'materials_id_owner_user_id_key',
            $ddl$UNIQUE (id, owner_user_id)$ddl$),
        (6, 'materials', 'materials_owner_user_id_request_key_key',
            $ddl$UNIQUE (owner_user_id, request_key)$ddl$),
        (7, 'modules', 'modules_id_created_by_user_id_key',
            $ddl$UNIQUE (id, created_by_user_id)$ddl$),
        (8, 'answers', 'answers_check',
            $ddl$CHECK (status = 'checked'::text AND score IS NOT NULL AND feedback IS NOT NULL AND checked_at IS NOT NULL OR status <> 'checked'::text AND score IS NULL AND feedback IS NULL AND checked_at IS NULL)$ddl$),
        (9, 'answers', 'answers_check1',
            $ddl$CHECK ((status = 'failed'::text) = (error_code IS NOT NULL))$ddl$),
        (10, 'answers', 'answers_response_check',
            $ddl$CHECK (jsonb_typeof(response) = 'object'::text)$ddl$),
        (11, 'answers', 'answers_score_check',
            $ddl$CHECK (score >= 0::numeric AND score <= 1::numeric)$ddl$),
        (12, 'answers', 'answers_status_check',
            $ddl$CHECK (status = ANY (ARRAY['pending'::text, 'checked'::text, 'failed'::text]))$ddl$),
        (13, 'exercises', 'exercises_answer_key_check',
            $ddl$CHECK (jsonb_typeof(answer_key) = 'object'::text)$ddl$),
        (14, 'exercises', 'exercises_content_check',
            $ddl$CHECK (jsonb_typeof(content) = 'object'::text)$ddl$),
        (15, 'exercises', 'exercises_instruction_check',
            $ddl$CHECK (length(btrim(instruction)) > 0)$ddl$),
        (16, 'exercises', 'exercises_position_check',
            $ddl$CHECK ("position" > 0)$ddl$),
        (17, 'exercises', 'exercises_question_text_check',
            $ddl$CHECK (length(btrim(question_text)) > 0)$ddl$),
        (18, 'exercises', 'exercises_type_check',
            $ddl$CHECK (type = ANY (ARRAY['vocabulary_gap'::text, 'vocabulary_matching_translation'::text, 'vocabulary_matching_definition'::text, 'vocabulary_context_choice'::text, 'grammar_brackets'::text, 'grammar_tense'::text, 'grammar_word_order'::text, 'comprehension_question'::text, 'reasoning_question'::text]))$ddl$),
        (19, 'llm_requests', 'llm_requests_attempt_count_check',
            $ddl$CHECK (attempt_count >= 0)$ddl$),
        (20, 'llm_requests', 'llm_requests_check',
            $ddl$CHECK (operation = 'prepare_material'::text AND material_id IS NOT NULL AND module_id IS NULL AND answer_id IS NULL OR operation = 'generate_module'::text AND material_id IS NULL AND module_id IS NOT NULL AND answer_id IS NULL OR operation = 'check_answer'::text AND material_id IS NULL AND module_id IS NULL AND answer_id IS NOT NULL)$ddl$),
        (21, 'llm_requests', 'llm_requests_check1',
            $ddl$CHECK (status = 'running'::text AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL AND attempt_count > 0 OR status <> 'running'::text AND lease_token IS NULL AND lease_expires_at IS NULL)$ddl$),
        (22, 'llm_requests', 'llm_requests_check2',
            $ddl$CHECK ((status = ANY (ARRAY['succeeded'::text, 'failed'::text])) = (finished_at IS NOT NULL))$ddl$),
        (23, 'llm_requests', 'llm_requests_check3',
            $ddl$CHECK ((status = 'failed'::text) = (error_code IS NOT NULL))$ddl$),
        (24, 'llm_requests', 'llm_requests_check4',
            $ddl$CHECK (status <> 'succeeded'::text OR result IS NOT NULL)$ddl$),
        (25, 'llm_requests', 'llm_requests_operation_check',
            $ddl$CHECK (operation = ANY (ARRAY['prepare_material'::text, 'generate_module'::text, 'check_answer'::text]))$ddl$),
        (26, 'llm_requests', 'llm_requests_parameters_check',
            $ddl$CHECK (jsonb_typeof(parameters) = 'object'::text)$ddl$),
        (27, 'llm_requests', 'llm_requests_prompt_version_check',
            $ddl$CHECK (length(btrim(prompt_version)) > 0)$ddl$),
        (28, 'llm_requests', 'llm_requests_result_check',
            $ddl$CHECK (jsonb_typeof(result) = 'object'::text)$ddl$),
        (29, 'llm_requests', 'llm_requests_status_check',
            $ddl$CHECK (status = ANY (ARRAY['queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text]))$ddl$),
        (30, 'materials', 'materials_check',
            $ddl$CHECK (kind = 'text'::text AND source_text IS NOT NULL AND length(btrim(source_text)) > 0 AND source_url IS NULL AND object_key IS NULL OR kind = 'url'::text AND source_url IS NOT NULL AND length(btrim(source_url)) > 0 AND source_text IS NULL AND object_key IS NULL OR (kind = ANY (ARRAY['file'::text, 'audio'::text])) AND object_key IS NOT NULL AND length(btrim(object_key)) > 0 AND source_text IS NULL AND source_url IS NULL AND content_type IS NOT NULL AND size_bytes IS NOT NULL AND content_sha256 IS NOT NULL)$ddl$),
        (31, 'materials', 'materials_check1',
            $ddl$CHECK (status <> 'ready'::text OR extracted_text IS NOT NULL AND length(btrim(extracted_text)) >= 1 AND length(btrim(extracted_text)) <= 200000 AND level IS NOT NULL)$ddl$),
        (32, 'materials', 'materials_check2',
            $ddl$CHECK ((status = 'failed'::text) = (error_code IS NOT NULL))$ddl$),
        (33, 'materials', 'materials_content_sha256_check',
            $ddl$CHECK (content_sha256 ~ '^[0-9a-f]{64}$'::text)$ddl$),
        (34, 'materials', 'materials_kind_check',
            $ddl$CHECK (kind = ANY (ARRAY['text'::text, 'file'::text, 'url'::text, 'audio'::text]))$ddl$),
        (35, 'materials', 'materials_level_check',
            $ddl$CHECK (level = ANY (ARRAY['A1'::text, 'A2'::text, 'B1'::text, 'B2'::text, 'C1'::text, 'C2'::text]))$ddl$),
        (36, 'materials', 'materials_size_bytes_check',
            $ddl$CHECK (size_bytes > 0)$ddl$),
        (37, 'materials', 'materials_status_check',
            $ddl$CHECK (status = ANY (ARRAY['pending'::text, 'ready'::text, 'failed'::text]))$ddl$),
        (38, 'materials', 'materials_title_check',
            $ddl$CHECK (length(btrim(title)) >= 1 AND length(btrim(title)) <= 200)$ddl$),
        (39, 'modules', 'modules_check',
            $ddl$CHECK ((status = 'ready'::text) = (published_at IS NOT NULL))$ddl$),
        (40, 'modules', 'modules_difficulty_check',
            $ddl$CHECK (difficulty = ANY (ARRAY['A1'::text, 'A2'::text, 'B1'::text, 'B2'::text, 'C1'::text, 'C2'::text]))$ddl$),
        (41, 'modules', 'modules_material_owner_check',
            $ddl$CHECK (material_id IS NULL OR created_by_user_id IS NOT NULL)$ddl$),
        (42, 'modules', 'modules_material_visibility_check',
            $ddl$CHECK (material_id IS NULL OR visibility = 'private'::text)$ddl$),
        (43, 'modules', 'modules_private_creator_check',
            $ddl$CHECK (visibility <> 'private'::text OR created_by_user_id IS NOT NULL)$ddl$),
        (44, 'modules', 'modules_status_check',
            $ddl$CHECK (status = ANY (ARRAY['draft'::text, 'generating'::text, 'ready'::text, 'failed'::text]))$ddl$),
        (45, 'modules', 'modules_title_check',
            $ddl$CHECK (length(btrim(title)) >= 1 AND length(btrim(title)) <= 200)$ddl$),
        (46, 'modules', 'modules_visibility_check',
            $ddl$CHECK (visibility = ANY (ARRAY['private'::text, 'public'::text]))$ddl$),
        (47, 'topics', 'topics_name_check',
            $ddl$CHECK (length(btrim(name)) >= 1 AND length(btrim(name)) <= 100)$ddl$),
        (48, 'topics', 'topics_slug_check',
            $ddl$CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'::text)$ddl$),
        (49, 'users', 'users_email_check',
            $ddl$CHECK (length(btrim(email)) >= 3 AND length(btrim(email)) <= 320)$ddl$),
        (50, 'users', 'users_name_check',
            $ddl$CHECK (length(btrim(name)) >= 1 AND length(btrim(name)) <= 100)$ddl$),
        (51, 'users', 'users_password_hash_check',
            $ddl$CHECK (length(btrim(password_hash)) > 0)$ddl$),
        (52, 'users', 'users_role_check',
            $ddl$CHECK (role = ANY (ARRAY['student'::text, 'teacher'::text, 'admin'::text]))$ddl$),
        (53, 'answers', 'answers_exercise_id_fkey',
            $ddl$FOREIGN KEY (exercise_id) REFERENCES public.exercises(id) ON DELETE CASCADE$ddl$),
        (54, 'answers', 'answers_user_id_fkey',
            $ddl$FOREIGN KEY (user_id) REFERENCES public.users(id)$ddl$),
        (55, 'exercises', 'exercises_module_id_fkey',
            $ddl$FOREIGN KEY (module_id) REFERENCES public.modules(id) ON DELETE CASCADE$ddl$),
        (56, 'llm_requests', 'llm_requests_answer_id_owner_user_id_fkey',
            $ddl$FOREIGN KEY (answer_id, owner_user_id) REFERENCES public.answers(id, user_id) ON DELETE CASCADE$ddl$),
        (57, 'llm_requests', 'llm_requests_material_id_owner_user_id_fkey',
            $ddl$FOREIGN KEY (material_id, owner_user_id) REFERENCES public.materials(id, owner_user_id) ON DELETE CASCADE$ddl$),
        (58, 'llm_requests', 'llm_requests_module_id_owner_user_id_fkey',
            $ddl$FOREIGN KEY (module_id, owner_user_id) REFERENCES public.modules(id, created_by_user_id) ON DELETE CASCADE$ddl$),
        (59, 'llm_requests', 'llm_requests_owner_user_id_fkey',
            $ddl$FOREIGN KEY (owner_user_id) REFERENCES public.users(id)$ddl$),
        (60, 'materials', 'materials_owner_user_id_fkey',
            $ddl$FOREIGN KEY (owner_user_id) REFERENCES public.users(id)$ddl$),
        (61, 'materials', 'materials_topic_id_fkey',
            $ddl$FOREIGN KEY (topic_id) REFERENCES public.topics(id)$ddl$),
        (62, 'modules', 'modules_created_by_user_id_fkey',
            $ddl$FOREIGN KEY (created_by_user_id) REFERENCES public.users(id)$ddl$),
        (63, 'modules', 'modules_material_id_created_by_user_id_fkey',
            $ddl$FOREIGN KEY (material_id, created_by_user_id) REFERENCES public.materials(id, owner_user_id) ON DELETE CASCADE$ddl$),
        (64, 'modules', 'modules_topic_id_fkey',
            $ddl$FOREIGN KEY (topic_id) REFERENCES public.topics(id)$ddl$),
        (65, 'catalog_materials', 'catalog_materials_type_check',
            $ddl$CHECK (type = ANY (ARRAY['text'::text, 'link'::text]))$ddl$),
        (66, 'catalog_materials', 'catalog_materials_level_check',
            $ddl$CHECK (level = ANY (ARRAY['A1'::text, 'A2'::text, 'B1'::text, 'B2'::text, 'C1'::text, 'C2'::text]))$ddl$),
        (67, 'catalog_materials', 'catalog_materials_title_check',
            $ddl$CHECK (length(btrim(title)) >= 1 AND length(btrim(title)) <= 200)$ddl$),
        (68, 'catalog_materials', 'catalog_materials_description_check',
            $ddl$CHECK (description IS NULL OR length(description) <= 500)$ddl$),
        (69, 'catalog_materials', 'catalog_materials_content_check',
            $ddl$CHECK (type = 'text'::text AND body IS NOT NULL AND length(btrim(body)) > 0 AND length(body) <= 100000 AND url IS NULL OR type = 'link'::text AND url IS NOT NULL AND length(url) <= 2048 AND url ~ '^https?://[^[:space:]]+$'::text AND body IS NULL)$ddl$),
        (70, 'catalog_materials', 'catalog_materials_topic_id_fkey',
            $ddl$FOREIGN KEY (topic_id) REFERENCES public.topics(id)$ddl$),
        (71, 'catalog_material_favorites', 'catalog_material_favorites_user_id_fkey',
            $ddl$FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE$ddl$),
        (72, 'catalog_material_favorites', 'catalog_material_favorites_material_id_fkey',
            $ddl$FOREIGN KEY (material_id) REFERENCES public.catalog_materials(id) ON DELETE CASCADE$ddl$)
        ) AS definitions(ordinal, table_name, constraint_name, definition)
        ORDER BY ordinal
    LOOP
        IF NOT EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE conrelid = format('public.%I', constraint_record.table_name)::regclass
              AND conname = constraint_record.constraint_name
        ) THEN
            EXECUTE format('ALTER TABLE public.%I ADD CONSTRAINT %I %s',
                constraint_record.table_name, constraint_record.constraint_name,
                constraint_record.definition);
        END IF;
    END LOOP;
END
$$;

-- migrate:statement

-- Standalone index: answers_user_exercise_idx
CREATE INDEX IF NOT EXISTS answers_user_exercise_idx ON public.answers USING btree (user_id, exercise_id, created_at DESC);

-- migrate:statement

-- Standalone index: llm_answer_active_unique
CREATE UNIQUE INDEX IF NOT EXISTS llm_answer_active_unique ON public.llm_requests USING btree (answer_id) WHERE ((answer_id IS NOT NULL) AND (status = ANY (ARRAY['queued'::text, 'running'::text])));

-- migrate:statement

-- Standalone index: llm_expired_lease_idx
CREATE INDEX IF NOT EXISTS llm_expired_lease_idx ON public.llm_requests USING btree (lease_expires_at) WHERE (status = 'running'::text);

-- migrate:statement

-- Standalone index: llm_material_active_unique
CREATE UNIQUE INDEX IF NOT EXISTS llm_material_active_unique ON public.llm_requests USING btree (material_id) WHERE ((material_id IS NOT NULL) AND (status = ANY (ARRAY['queued'::text, 'running'::text])));

-- migrate:statement

-- Standalone index: llm_module_active_unique
CREATE UNIQUE INDEX IF NOT EXISTS llm_module_active_unique ON public.llm_requests USING btree (module_id) WHERE ((module_id IS NOT NULL) AND (status = ANY (ARRAY['queued'::text, 'running'::text])));

-- migrate:statement

-- Standalone index: llm_owner_idx
CREATE INDEX IF NOT EXISTS llm_owner_idx ON public.llm_requests USING btree (owner_user_id, created_at DESC);

-- migrate:statement

-- Standalone index: llm_queue_idx
CREATE INDEX IF NOT EXISTS llm_queue_idx ON public.llm_requests USING btree (available_at, created_at) WHERE (status = 'queued'::text);

-- migrate:statement

-- Standalone index: materials_owner_idx
CREATE INDEX IF NOT EXISTS materials_owner_idx ON public.materials USING btree (owner_user_id, created_at DESC);

-- migrate:statement

-- Standalone index: modules_catalog_idx
CREATE INDEX IF NOT EXISTS modules_catalog_idx ON public.modules USING btree (topic_id, difficulty) WHERE (status = 'ready'::text);

-- migrate:statement

-- Standalone index: modules_creator_idx
CREATE INDEX IF NOT EXISTS modules_creator_idx ON public.modules USING btree (created_by_user_id, created_at DESC);

-- migrate:statement

-- Standalone index: modules_material_idx
CREATE INDEX IF NOT EXISTS modules_material_idx ON public.modules USING btree (material_id) WHERE (material_id IS NOT NULL);

-- migrate:statement

-- Standalone index: users_email_unique
CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique ON public.users USING btree (lower(btrim(email)));

-- migrate:statement

-- Guard function: guard_answer_write
CREATE OR REPLACE FUNCTION public.guard_answer_write()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE module_status text; module_owner bigint; module_visibility text;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.id <> OLD.id OR NEW.user_id <> OLD.user_id OR NEW.exercise_id <> OLD.exercise_id
            OR NEW.response IS DISTINCT FROM OLD.response OR NEW.request_key <> OLD.request_key THEN
            RAISE EXCEPTION 'submitted response is immutable; submit a new answer';
        END IF;
        IF OLD.status = 'checked' AND NEW IS DISTINCT FROM OLD THEN
            RAISE EXCEPTION 'checked answer is immutable';
        END IF;
        RETURN NEW;
    END IF;
    SELECT m.status, m.created_by_user_id, m.visibility
        INTO module_status, module_owner, module_visibility
        FROM public.exercises e JOIN public.modules m ON m.id = e.module_id
        WHERE e.id = NEW.exercise_id FOR SHARE OF m;
    IF NOT FOUND OR module_status <> 'ready' THEN RAISE EXCEPTION 'answer requires a ready module'; END IF;
    IF module_visibility = 'private' AND module_owner IS DISTINCT FROM NEW.user_id THEN
        RAISE EXCEPTION 'private module belongs to another user';
    END IF;
    RETURN NEW;
END $function$;

-- migrate:statement

-- Guard function: guard_exercise_write
CREATE OR REPLACE FUNCTION public.guard_exercise_write()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE module_status text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        SELECT status INTO module_status FROM public.modules WHERE id = OLD.module_id FOR UPDATE;
        IF FOUND AND module_status = 'ready' THEN
            RAISE EXCEPTION 'delete the whole published module, not one exercise';
        END IF;
        RETURN OLD; -- During whole-module cascade the parent is already removed.
    END IF;
    IF TG_OP = 'UPDATE' AND (NEW.id <> OLD.id OR NEW.module_id <> OLD.module_id) THEN
        RAISE EXCEPTION 'exercise identity and module are immutable';
    END IF;
    SELECT status INTO module_status FROM public.modules WHERE id = NEW.module_id FOR UPDATE;
    IF NOT FOUND OR module_status = 'ready' THEN
        RAISE EXCEPTION 'exercise requires an unpublished module';
    END IF;
    RETURN NEW;
END $function$;

-- migrate:statement

-- Guard function: guard_module_write
CREATE OR REPLACE FUNCTION public.guard_module_write()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE exercise_count integer; creator_role text;
BEGIN
    IF TG_OP = 'INSERT' AND NEW.status = 'ready' THEN
        RAISE EXCEPTION 'create a draft module before publication';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.id <> OLD.id OR NEW.created_by_user_id IS DISTINCT FROM OLD.created_by_user_id
            OR NEW.material_id IS DISTINCT FROM OLD.material_id THEN
            RAISE EXCEPTION 'module identity and source are immutable';
        END IF;
        IF OLD.status = 'ready' AND NEW IS DISTINCT FROM OLD THEN
            RAISE EXCEPTION 'published module is immutable; create a copy';
        END IF;
    END IF;
    -- A NULL creator denotes an internally managed system/catalog module.
    -- Authorization for managing such modules is enforced by the admin service.
    IF NEW.visibility = 'public' AND NEW.created_by_user_id IS NOT NULL THEN
        SELECT role INTO creator_role FROM public.users WHERE id = NEW.created_by_user_id;
        IF creator_role IS NULL OR creator_role NOT IN ('teacher', 'admin') THEN
            RAISE EXCEPTION 'only a teacher or admin can create a public module';
        END IF;
    END IF;
    IF NEW.status = 'ready' THEN
        SELECT count(*) INTO exercise_count FROM public.exercises WHERE module_id = NEW.id;
        IF exercise_count < 2 THEN RAISE EXCEPTION 'module requires at least two exercises'; END IF;
        IF EXISTS (SELECT 1 FROM public.exercises WHERE module_id = NEW.id
            AND (answer_key = '{}'::jsonb OR length(btrim(explanation)) = 0)) THEN
            RAISE EXCEPTION 'published exercises require answer keys and explanations';
        END IF;
    END IF;
    RETURN NEW;
END $function$;

-- migrate:statement

-- Install guards once; CREATE OR REPLACE above updates their function bodies.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.answers'::regclass
          AND tgname = 'answers_write_guard'
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER answers_write_guard BEFORE INSERT OR UPDATE ON public.answers FOR EACH ROW EXECUTE FUNCTION public.guard_answer_write();
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.exercises'::regclass
          AND tgname = 'exercises_write_guard'
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER exercises_write_guard BEFORE INSERT OR DELETE OR UPDATE ON public.exercises FOR EACH ROW EXECUTE FUNCTION public.guard_exercise_write();
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.modules'::regclass
          AND tgname = 'modules_write_guard'
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER modules_write_guard BEFORE INSERT OR UPDATE ON public.modules FOR EACH ROW EXECUTE FUNCTION public.guard_module_write();
    END IF;
END
$$;

-- migrate:statement

-- Standalone index: catalog_materials_topic_level_idx
CREATE INDEX IF NOT EXISTS catalog_materials_topic_level_idx ON public.catalog_materials USING btree (topic_id, level, id DESC);

-- migrate:statement

-- Standalone index: catalog_favorites_material_idx
CREATE INDEX IF NOT EXISTS catalog_favorites_material_idx ON public.catalog_material_favorites USING btree (material_id);