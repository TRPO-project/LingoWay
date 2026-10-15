# Общая схема LingoWay v0.2

Актуальное описание: [DATABASE.md](../DATABASE.md).

- [SQL общей БД](../schema.sql) — семь таблиц public.*.
- [Шаблоны заданий](../exercise-templates.json) и [девять примеров](../exercise-examples.json).
- [API v0.2](openapi.json).
- [Схема БД PNG](images/be3-database.png) и [поток API PNG](images/be3-api-flow.png).

Прототип из 14 таблиц заменён общей схемой: модули непосредственно содержат задания, а очередь и журнал LLM объединены в llm_requests. Старые exercise_sets, attempts/jobs и отдельные таблицы кэша, промптов и прогресса в текущей модели не используются.
