# Модуль материалов и подключения LLM

Модуль принимает английский текст, сохраняет его в PostgreSQL и через языковую модель определяет сложность по шкале CEFR от A1 до C2. По готовому материалу можно сгенерировать лексические и грамматические упражнения.

Полный путь от запуска сервера до получения упражнений показан в одном примере. [Сценарий в PowerShell](#пример-полного-сценария-в-powershell)

## Текущий режим работы

Маршруты материалов, генерации и чтения сгенерированных заданий работают в общем режиме без JWT. Любой клиент с ID может прочитать материал, статус обработки или упражнения готового сгенерированного модуля.

Обязательные связи с таблицей `users` сохранены для целостности БД. При создании материала сервис сам назначает общего технического владельца. Клиент не передаёт ID пользователя и не создаёт эту учётную запись вручную. Технический владелец связывает записи БД, но не даёт разграничения доступа между клиентами.

Для каждого нового POST создавайте случайный UUID. Храните ключ вместе с адресом и телом запроса, чтобы при повторной отправке получить прежнюю пару записей. [Как повторять запрос](#повторная-отправка-и-общий-ключ)

Работают приём текста, предварительный импорт TXT/Markdown/DOCX, определение CEFR, генерация закрытых упражнений и ограниченные повторы ошибок.

## Короткий словарь

| Термин | Значение в модуле |
|---|---|
| LLM | Языковая модель у настроенного провайдера. Оценивает сложность материала и составляет упражнения |
| CEFR | Шкала английского языка от A1 до C2. Оценивается материал, а не уровень конкретного пользователя |
| Очередь | Строки `llm_requests` в PostgreSQL, ожидающие обработки; отдельного брокера сообщений нет |
| Воркер | Фоновый обработчик, который берёт запрос из очереди и вызывает модель |
| Транзакция | Несколько изменений БД, которые сохраняются вместе либо полностью отменяются |
| Идемпотентность | Повтор с тем же UUID и вводом не создаёт второй материал и второй запрос |
| Аренда | Временное право воркера завершить запрос: случайный `lease_token` и срок `lease_expires_at` |
| DTO | Структура ответа API с разрешёнными полями, которая отличается от полной модели БД |
| Технический владелец | Общая служебная запись пользователя для обязательных связей БД; не авторизация клиента |

## Запуск

Понадобятся Go версии из `backend/go.mod` и PostgreSQL. В проекте PostgreSQL запускается через Docker Compose; также можно указать уже работающую БД.

Из корня репозитория создайте `.env`, если его ещё нет, и запустите БД.

```powershell
if (!(Test-Path .env)) { Copy-Item .env.example .env }
docker compose up -d db
```

Задайте в `.env` адрес PostgreSQL и настройки провайдера. Вместо `your-provider-key` укажите рабочий ключ в локальном файле.

```dotenv
DATABASE_URL=postgres://postgres:password@localhost:5432/lingoway
LLM_API_KEYS=your-provider-key
LLM_BASE_URL=https://api.openai.com/v1
LLM_MODEL=gpt-4o-mini
```

Данные PostgreSQL должны совпадать с `DB_USER`, `DB_PASSWORD`, `DB_NAME` из того же файла. Если `.env` уже существует, меняйте нужные значения в нём, не копируя пример поверх рабочих настроек.

Запустите сервер.

```powershell
cd backend
go run ./cmd
```

Приложение загружает настройки, подключается к БД и при `DB_AUTO_MIGRATE=true` проверяет схему и добавляет стартовые темы. После этого оно запускает HTTP-сервер и воркеры LLM при наличии ключей.

В другом окне PowerShell проверьте доступность сервера.

```powershell
Invoke-RestMethod http://localhost:8080/api/health
```

Ожидаемый ответ — `{"status":"ok"}`. Работоспособность ключа и модели можно проверить первым запросом на подготовку материала.

Для запуска БД и бэкенда в контейнерах из корня репозитория используется `docker compose up --build`. В контейнере адрес PostgreSQL переопределяется на сервис `db`.

## Настройки

Настройки читаются один раз при старте. При локальном запуске дополнительно загружаются `.env` в текущем каталоге и `../.env`. Уже установленные переменные процесса не перезаписываются; значение из первого найденного файла также имеет приоритет над вторым. После изменения настроек сервер нужно перезапустить.

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `PORT` | `8080` | Порт нашего HTTP-сервера |
| `DATABASE_URL` | Обязательна | Подключение к PostgreSQL |
| `DB_AUTO_MIGRATE` | `true` | Проверка и дополнение схемы перед запуском HTTP |
| `LLM_API_KEYS` | Пусто | Ключи одного провайдера через запятую; используются по очереди |
| `LLM_BASE_URL` | `https://api.openai.com/v1` | Базовый адрес API; клиент добавляет `/chat/completions` |
| `LLM_MODEL` | `gpt-4o-mini` | Имя модели у выбранного провайдера |
| `LLM_TIMEOUT` | `45s` | Предельное время одного вызова модели |
| `LLM_POLL_INTERVAL` | `1s` | Ожидание при пустой очереди или ошибке её чтения |
| `LLM_MAX_ATTEMPTS` | `3` | Бюджет попыток одного запроса, включая первую; допустимо 1–10 |

Дополнительные настройки PostgreSQL перечислены в примере окружения. [Пример .env](../.env.example)

Провайдер должен поддерживать формат Chat Completions и режим ответа JSON. Базовый адрес использует HTTPS; HTTP разрешён только для `localhost` или адресов этого же компьютера, например `127.0.0.1` и `::1`. В адресе запрещены логин, пароль, параметры запроса и фрагмент после `#`. Перенаправления не выполняются, чтобы ключ не передавался на другой адрес.

Без `LLM_API_KEYS` сервер запускается. Новый запрос на подготовку или генерацию получает `503 SERVICE_UNAVAILABLE`. Доступны извлечение текста, чтение сохранённых записей и повтор уже принятого запроса с прежним ключом и вводом. Воркеры продолжают очередь после запуска с настроенным провайдером.

## Как проходит обработка

1. Запустите PostgreSQL и сервер с настроенным ключом провайдера. Подготовьте английский текст. Для TXT, Markdown или DOCX можно сначала вызвать `POST /api/materials/extract`. Возьмите поле `text` из результата предварительного просмотра. Запись с ID появится на следующем шаге. [Запуск](#запуск) · [Импорт файла](#предварительный-импорт-файла)
2. Отправьте `POST /api/materials` с `kind`, `title`, `text` и новым UUID в заголовке `Idempotency-Key`. Сервер сам присвоит ID материалу и запросу подготовки. HTTP 202 сразу вернёт `material.id` и `llm_request.id`. Сохраните оба числа. [Тело и ответ POST](#создание-материала)
3. Опрашивайте `GET /api/llm-requests/{request_id}` по ID запроса подготовки. Когда он станет `succeeded`, прочитайте `GET /api/materials/{material_id}`. После успеха материал уже в `ready`, а в ответе есть уровень CEFR в `level` и нормализованный текст в `extracted_text`. [Статусы и ошибки](#чтение-статуса-и-результата)
4. Выберите существующую тему и отправьте `POST /api/materials/{material_id}/modules`. В адрес подставьте `material.id` из шага 2. Укажите в теле `title`, `topic_id`, `difficulty`, `count` и `types`. Для этого POST создайте отдельный UUID. В HTTP 202 будут `module.id` и ID нового запроса в `llm_request.id`. Подготовленный текст воркер загрузит по ID материала. [Тело и ответ генерации](#генерация-лексики-и-грамматики)
5. Опрашивайте тот же маршрут `GET /api/llm-requests/{request_id}` по новому `llm_request.id` из шага 4. При `succeeded` модуль готов. При `failed` остановите опрос и прочитайте `error.code` и `error.message`.
6. Получите упражнения через `GET /api/modules/{module_id}/exercises`. В адрес подставьте `module.id` из шага 4. При `has_more: true` увеличьте `offset` на значение `limit` и запросите следующую страницу. [Формат выдачи](#чтение-упражнений)

### Откуда берутся ID

| Значение | Откуда взять | Куда передать |
|---|---|---|
| ID материала, например `12` | `material.id` в ответе на `POST /api/materials` | `GET /api/materials/12` и `POST /api/materials/12/modules` |
| ID запроса подготовки, например `34` | `llm_request.id` в ответе на `POST /api/materials` | `GET /api/llm-requests/34` |
| ID темы, например `1` | ID существующей строки из `topics` | В JSON-поле `topic_id` при генерации. При создании материала поле необязательное |
| ID модуля, например `7` | `module.id` в ответе на `POST /api/materials/12/modules` | `GET /api/modules/7/exercises` |
| ID запроса генерации, например `35` | `llm_request.id` в ответе на `POST /api/materials/12/modules` | `GET /api/llm-requests/35` |
| ID упражнения | `items[].id` в ответе на чтение упражнений | Идентификатор отдельного упражнения в ответе |
| `Idempotency-Key` | Клиент создаёт случайный UUID перед новой операцией | Только заголовок POST. Для повтора того же запроса используется сохранённый UUID |

Числа в таблице условные. В URL подставляйте значение из своего ответа без фигурных скобок.

```mermaid
flowchart TD
    F["TXT / MD / DOCX"] --> X["POST /api/materials/extract"]
    X -->|text| C["POST /api/materials"]
    T["Английский текст"] --> C
    C -->|llm_request.id| P["Опрос запроса подготовки"]
    P -->|succeeded| R["GET /api/materials/{material_id}"]
    R -->|material.id и level| G["POST /api/materials/{material_id}/modules"]
    G -->|новый llm_request.id| Q["Опрос запроса генерации"]
    Q -->|succeeded| E["GET /api/modules/{module_id}/exercises"]
    P -->|failed| Z["Прочитать код ошибки"]
    Q -->|failed| Z
```

## HTTP API

Все ID ресурсов — положительные целые числа. UUID используется в заголовке повторной отправки. Маршруты модуля работают в общем режиме и возвращают `Cache-Control: private, no-store`.

| Метод и путь | Результат |
|---|---|
| `POST /api/materials` | `202`: объекты `material` и `llm_request` |
| `GET /api/materials/{material_id}` | `200`: материал, а после успеха также `level` и `extracted_text` |
| `GET /api/llm-requests/{request_id}` | `200`: состояние запроса, связанные ID и ошибка обработки при её наличии |
| `POST /api/materials/extract` | `200`: текст загруженного файла и число символов; ничего не сохраняется |
| `POST /api/materials/{material_id}/modules` | `202`: объекты `module` и `llm_request` |
| `GET /api/modules/{module_id}/exercises` | `200`: страница заданий готового сгенерированного модуля |

### Создание материала

`POST /api/materials` принимает `Content-Type: application/json` и обязательный UUID в `Idempotency-Key`. Сервер создаёт материал и присваивает ему ID. Отправьте поля из таблицы.

| Поле JSON | Обязательно | Содержание |
|---|---|---|
| `kind` | Да | Только `"text"` |
| `title` | Да | Название длиной 1–200 символов после удаления внешних пробелов |
| `text` | Да | UTF-8 текст длиной 1–20 000 символов после нормализации |
| `topic_id` | Нет | Положительный ID существующей темы; если не передан, тема не назначается |

Неизвестные поля отклоняются. Нормализация убирает начальную метку кодировки BOM, заменяет переносы Windows CRLF на LF и удаляет пробелы по краям. Внутренние абзацы сохраняются. Пустой текст, неверный UTF-8 и управляющие символы, кроме переноса строки и табуляции, не принимаются. Превышение лимита возвращает ошибку, а не сокращённый материал.

Пример HTTP-запроса:

```http
POST /api/materials HTTP/1.1
Content-Type: application/json
Idempotency-Key: 16c061c4-b373-42b5-b4dc-6fe247aeeb04

{"kind":"text","title":"Algorithms","text":"An algorithm is a set of steps to solve a problem."}
```

Пример ответа HTTP 202:

```json
{
  "material": {
    "id": 12,
    "title": "Algorithms",
    "topic_id": null,
    "kind": "text",
    "status": "pending",
    "level": null,
    "extracted_text": null,
    "created_at": "2026-10-28T10:00:00Z"
  },
  "llm_request": {
    "id": 34,
    "operation": "prepare_material",
    "status": "queued",
    "material_id": 12,
    "module_id": null,
    "answer_id": null,
    "error": null,
    "created_at": "2026-10-28T10:00:00Z",
    "finished_at": null
  }
}
```

HTTP 202 означает, что сервер принял запрос. При первом POST материал и запрос уже сохранены, а подготовка поставлена в очередь. `material.id` идентифицирует текст, `llm_request.id` идентифицирует работу по его подготовке. В этом ответе `module_id` и `answer_id` равны `null`.

### Повторная отправка и общий ключ

Перед новой операцией создайте UUID и сохраните его вместе с адресом и телом POST. Если соединение оборвалось до получения ответа, повторите тот же POST с прежним UUID, адресом и телом. Сервер вернёт исходные ID и текущие статусы, если первый запрос уже сохранён.

Для создания материала сравниваются нормализованные `title`, `text`, `kind` и `topic_id`. Для генерации сравниваются ID материала в адресе, название, тема, уровень, количество и порядок типов упражнений. Тот же ключ с другим вводом в рамках одной операции и владельца возвращает HTTP 409.

```json
{
  "error": {
    "code": "IDEMPOTENCY_CONFLICT",
    "message": "Этот Idempotency-Key уже использован с другими данными."
  }
}
```

Проверка общая для всех клиентов одного владельца. Подготовка и генерация используют отдельные пространства ключей. Сервис блокирует одновременное создание по владельцу, операции и ключу до конца транзакции. Второй запрос ждёт первый, затем читает сохранённую пару.

### Чтение статуса и результата

Для примера выше запрос подготовки имеет ID `34`, а материал — `12`.

```http
GET /api/llm-requests/34 HTTP/1.1
```

```http
GET /api/materials/12 HTTP/1.1
```

Опрашивайте состояние раз в 2–3 секунды. Пока оно `queued` или `running`, работа не завершена. После `succeeded` или `failed` опрос нужно остановить.

Пример готового материала:

```json
{
  "id": 12,
  "title": "Algorithms",
  "topic_id": null,
  "kind": "text",
  "status": "ready",
  "level": "B1",
  "extracted_text": "An algorithm is a set of steps to solve a problem.",
  "created_at": "2026-10-28T10:00:00Z"
}
```

Уровень B1 здесь условный: фактическая оценка зависит от модели. Исходный текст сохраняется целиком.

Пример успешного запроса подготовки:

```json
{
  "id": 34,
  "operation": "prepare_material",
  "status": "succeeded",
  "material_id": 12,
  "module_id": null,
  "answer_id": null,
  "error": null,
  "created_at": "2026-10-28T10:00:00Z",
  "finished_at": "2026-10-28T10:00:03Z"
}
```

Пример запроса, окончательно завершившегося ошибкой:

```json
{
  "id": 34,
  "operation": "prepare_material",
  "status": "failed",
  "material_id": 12,
  "module_id": null,
  "answer_id": null,
  "error": {
    "code": "LLM_TIMEOUT",
    "message": "Сервис модели временно недоступен.",
    "retryable": true
  },
  "created_at": "2026-10-28T10:00:00Z",
  "finished_at": "2026-10-28T10:00:20Z"
}
```

GET статуса возвращает HTTP 200 и при `failed`, потому что прочитать запись удалось. `retryable=true` описывает временную причину сбоя. При `failed` сервер уже закончил автоматические попытки. Повтор POST со старым ключом вернёт ту же запись. Чтобы запустить подготовку заново, отправьте `POST /api/materials` с новым UUID. Чтобы заново сгенерировать упражнения по готовому тексту, отправьте `POST /api/materials/{material_id}/modules` с новым UUID. В обоих случаях сервер создаст новую пару записей.

Основные коды фоновой ошибки:

| `error.code` | Причина | `retryable` |
|---|---|---|
| `LLM_TIMEOUT` | Истекло время обращения к модели | `true` |
| `LLM_RATE_LIMITED` | Провайдер ограничил частоту запросов | `true` |
| `LLM_UNAVAILABLE` | Сетевая ошибка или временная ошибка провайдера | `true` |
| `LLM_INVALID_RESPONSE` | Ответ не прошёл проверку CEFR либо структуры, числа или ключей упражнений | `false` |
| `LLM_PROVIDER_ERROR` | Провайдер отклонил запрос постоянной ошибкой | `false` |
| `PROCESSING_FAILED` | Другой служебный сбой, для которого API использует общее описание | `false` |

### Предварительный импорт файла

`POST /api/materials/extract` принимает ровно один файл в multipart-поле `file`. Пример для PowerShell с системным curl:

```powershell
curl.exe -X POST http://localhost:8080/api/materials/extract -F "file=@material.docx"
```

Ответ:

```json
{"text":"An algorithm is a set of steps to solve a problem.","characters":50}
```

Файл ограничен 2 МиБ, извлечённый текст — 20 000 символами. Дополнительные поля и несколько файлов отклоняются. TXT и Markdown должны быть в UTF-8. DOCX читается как ZIP/XML: извлекаются основной текст, абзацы, переносы, табуляция и текст таблиц. Внешние ссылки не загружаются, поля Word не исполняются. Повреждённые и зашифрованные архивы, дубликаты основного XML и недопустимый XML отклоняются. Изображения, распознавание текста на изображениях, колонтитулы и сложная вёрстка не превращаются в учебный текст.

Этот вызов только возвращает текст для предварительного просмотра. Он не вызывает LLM, не создаёт записи в БД и не сохраняет бинарный источник. После проверки передайте текст отдельным запросом в `POST /api/materials`: будет создан `kind=text`, а не `kind=file`. Для импорта заголовок `Idempotency-Key` не требуется.

### Ошибки HTTP-запросов

Они имеют форму `{"error":{"code":"...","message":"..."}}` без поля `retryable`. Это ошибки самого HTTP-ввода или работы сервиса; их нужно отличать от `error` внутри успешно прочитанного запроса LLM.

| HTTP | Коды | Когда возвращается |
|---|---|---|
| `400` | `INVALID_JSON`, `INVALID_ID`, `INVALID_IDEMPOTENCY_KEY`, `INVALID_MULTIPART` | Неверный JSON, ID, UUID ключа или структура multipart |
| `400` | `INVALID_PAGINATION` | Некорректные `limit` или `offset` при чтении упражнений |
| `404` | `NOT_FOUND` | Не найдены материал, запрос LLM, тема или модуль, созданный генерацией |
| `409` | `IDEMPOTENCY_CONFLICT` | Прежний ключ использован с другим вводом |
| `409` | `MATERIAL_NOT_READY`, `MODULE_NOT_READY` | Материал пока недоступен для генерации или модуль пока недоступен для чтения |
| `413` | `MATERIAL_TOO_LARGE`, `REQUEST_TOO_LARGE` | Превышен размер текста, файла или тела запроса |
| `415` | `UNSUPPORTED_MEDIA_TYPE`, `UNSUPPORTED_FILE_FORMAT` | Неподдерживаемый Content-Type либо расширение файла |
| `422` | `INVALID_TEXT`, `INVALID_FILE`, `INVALID_INPUT`, `UNSUPPORTED_MATERIAL_KIND` | Неверные данные материала, файла или параметры генерации |
| `503` | `SERVICE_UNAVAILABLE` | Недоступен сервис обработки или для новой работы отсутствуют ключи LLM |
| `500` | `INTERNAL_ERROR` | Неизвестная внутренняя ошибка без подробностей БД или провайдера |

Ошибки `401` и `AUTH_NOT_CONFIGURED` не относятся к текущим маршрутам материалов: проверка JWT отключена.

## Два набора статусов

Статус ресурса показывает, можно ли использовать материал или упражнения. Статус запроса LLM показывает ход фоновой работы.

| Ситуация | Материал при подготовке | Модуль при генерации | Запрос LLM |
|---|---|---|---|
| Данные приняты | `pending` | `generating` | `queued` |
| Выполняется вызов модели | `pending` | `generating` | `running` |
| Ожидается повтор | `pending` | `generating` | `queued` |
| Работа завершилась успешно | `ready` | `ready` | `succeeded` |
| Окончательная ошибка | `failed` | `failed` | `failed` |

Пока материал в `pending`, его `level` и `extracted_text` ещё не готовы. Пока модуль в `generating`, чтение упражнений возвращает `409 MODULE_NOT_READY`. При завершении воркер обновляет ресурс и запрос LLM одной транзакцией.

## Ограничения размера

| Объект | Предел |
|---|---|
| Название | 1–200 символов после удаления внешних пробелов |
| Текст нового материала или результат импорта | 1–20 000 символов Unicode после нормализации |
| TXT, MD или сжатый DOCX | 2 МиБ |
| Полное multipart-тело | 2 МиБ + 64 КиБ на служебные данные |
| Распакованный `word/document.xml` | 8 МиБ |
| Число записей DOCX-архива | Не более 1024 |
| Тело запроса генерации | 8 КиБ |
| Ответ HTTP провайдера при оценке CEFR | 64 КиБ |
| Ответ HTTP провайдера при генерации упражнений | 512 КиБ |
| Аренда работы | `LLM_TIMEOUT + 30s` по времени PostgreSQL |

Размер файла считается в байтах, длина текста — в кодовых точках Unicode. `MaxMaterialTextCharacters` находится в `text_validation.go`, `MaxTextImportFileBytes` — в `text_import.go`; ограничения DOCX — в `docx_text_extractor.go`.

Общая схема БД и дополнительная защита клиента допускают текст до 200 000 символов. Это предел общего слоя, а не доступный размер нового материала: текущий приём ограничен 20 000. Разбиение на фрагменты пока не реализовано. При увеличении лимита нужно отдельно учитывать объём контекста выбранной модели и стоимость полного обращения.

## Очередь, повторы и восстановление

В каждом процессе `PreparationWorker` выбирает запросы `prepare_material`, а `ModuleGenerationWorker` выбирает `generate_module`. После обработанного запроса воркер сразу ищет следующий. При отсутствии работы или ошибке БД он ждёт `LLM_POLL_INTERVAL`. Несколько процессов могут использовать одну БД. `FOR UPDATE SKIP LOCKED` позволяет им выбирать разные свободные строки.

Захват запроса увеличивает `attempt_count`, создаёт новый случайный токен и срок аренды. Счётчик отражает получение работы воркером: процесс может упасть до обращения к модели. Это не точное число оплаченных запросов.

Клиент модели делает не более одного HTTP-вызова за попытку и не выполняет скрытых немедленных повторов. Ключи выбираются по кругу между вызовами. Постоянная ошибка, например HTTP 401 у провайдера, завершает работу, а не запускает перебор всех ключей.

Таймаут, HTTP 408/429, ответы 5xx и сетевые ошибки считаются временными. Пока есть попытки, запрос возвращается в `queued`, материал остаётся `pending`, а модуль — `generating` на своём этапе. Базовая задержка растёт от 2 до 32 секунд. Если провайдер передал `Retry-After` в секундах или в виде даты HTTP, сервер выбирает большую из двух задержек и записывает время следующей попытки в БД. Подсказка провайдера ограничена 24 часами. При стандартных трёх попытках и без подсказки первая ошибка даёт паузу 2 секунды, вторая даёт 4 секунды, третья завершает работу. Неверный JSON, недопустимый CEFR, некорректные задания и постоянные ошибки завершают обработку сразу.

Время аренды берётся у PostgreSQL, чтобы разные часы серверов приложения не влияли на результат. Если процесс прекратился, запрос может остаться `running`. После истечения аренды другой воркер заберёт его с новым токеном. Перед сохранением проверяются токен и срок, в том числе при последнем UPDATE. Потеря аренды откатывает всю транзакцию.

Внешний HTTP-вызов не входит в транзакцию БД. Если модель ответила, а процесс упал до сохранения, после восстановления возможен новый вызов. Аренда защищает согласованность БД, но не гарантирует единственное обращение к провайдеру. Повторные захваты также ограничены бюджетом попыток.

При штатной остановке контексты воркеров отменяются. Незавершённый запрос можно восстановить после истечения аренды. Приложение ждёт остановки обоих воркеров перед закрытием пула БД.

## Где находится код

`httpapi` занимается HTTP-вводом и ответами. `materials` управляет источником, его сохранением и очередью. `generation` отвечает за обращение к модели. SQL очереди отделён от последовательности шагов воркера.

| Файл | Ответственность |
|---|---|
| [`backend/cmd/main.go`](../backend/cmd/main.go) | Настройки, миграции, запуск клиента, сервисов, воркеров и HTTP |
| [`backend/internal/config/config.go`](../backend/internal/config/config.go) | Загрузка окружения и общие параметры |
| [`backend/internal/config/llm.go`](../backend/internal/config/llm.go) | Проверка параметров провайдера и ограничений |
| [`backend/internal/httpapi/router.go`](../backend/internal/httpapi/router.go) | Подключение маршрутов материалов и генерации в общем режиме |
| [`backend/internal/httpapi/material_handlers.go`](../backend/internal/httpapi/material_handlers.go) | HTTP-ввод, вызов сервиса и предварительный импорт |
| [`backend/internal/httpapi/material_http_responses.go`](../backend/internal/httpapi/material_http_responses.go) | DTO, известные коды и безопасные описания ошибок |
| [`backend/internal/httpapi/module_generation_handlers.go`](../backend/internal/httpapi/module_generation_handlers.go) | Запуск генерации и выдача страниц упражнений |
| [`backend/internal/materials/material_service.go`](../backend/internal/materials/material_service.go) | Создание материала и запроса, общий технический владелец, идемпотентность, чтение по ID |
| [`backend/internal/materials/module_generation_service.go`](../backend/internal/materials/module_generation_service.go) | Создание модуля и запроса генерации, повтор POST и чтение готовых упражнений |
| [`backend/internal/materials/shared_material_owner.go`](../backend/internal/materials/shared_material_owner.go) | Поиск или создание общей служебной учётной записи внутри транзакции создания материала |
| [`backend/internal/materials/text_validation.go`](../backend/internal/materials/text_validation.go) | Нормализация и проверка текста |
| [`backend/internal/materials/text_import.go`](../backend/internal/materials/text_import.go) | Выбор формата и ограниченное чтение файла |
| [`backend/internal/materials/docx_text_extractor.go`](../backend/internal/materials/docx_text_extractor.go) | Разбор ZIP/XML и извлечение основного текста Word |
| [`backend/internal/materials/preparation_worker.go`](../backend/internal/materials/preparation_worker.go) | Получить работу, определить CEFR и сохранить итог |
| [`backend/internal/materials/module_generation_worker.go`](../backend/internal/materials/module_generation_worker.go) | Получить работу, загрузить готовый текст и вызвать генерацию |
| [`backend/internal/materials/llm_request_queue.go`](../backend/internal/materials/llm_request_queue.go) | Общие операции захвата, аренды, назначения повтора и завершения запроса |
| [`backend/internal/materials/preparation_queue.go`](../backend/internal/materials/preparation_queue.go) | Транзакционное сохранение итога подготовки материала |
| [`backend/internal/materials/module_generation_queue.go`](../backend/internal/materials/module_generation_queue.go) | Транзакционная публикация упражнений и итога генерации |
| [`backend/internal/generation/chat_completion_client.go`](../backend/internal/generation/chat_completion_client.go) | Общий HTTP-клиент, выбор ключа и таймаут |
| [`backend/internal/generation/cefr_level_client.go`](../backend/internal/generation/cefr_level_client.go) | Выбор инструкции оценки CEFR и отправка текста |
| [`backend/internal/generation/chat_completion_response.go`](../backend/internal/generation/chat_completion_response.go) | Ограниченное чтение и разбор ответа провайдера |
| [`backend/internal/generation/exercise_generation_client.go`](../backend/internal/generation/exercise_generation_client.go) | Формирование запроса на генерацию |
| [`backend/internal/generation/generated_exercises.go`](../backend/internal/generation/generated_exercises.go) | Распределение типов и проверка набора упражнений |
| [`backend/internal/generation/prompts/prepare-material-v1.txt`](../backend/internal/generation/prompts/prepare-material-v1.txt) | Инструкция для ранее поставленных запросов |
| [`backend/internal/generation/prompts/prepare-material-v2.txt`](../backend/internal/generation/prompts/prepare-material-v2.txt) | Критерии оценки A1–C2 для новых запросов |
| [`backend/internal/generation/prompts/generate-module-v1.txt`](../backend/internal/generation/prompts/generate-module-v1.txt) | Инструкция генерации упражнений |
| [`backend/internal/generation/schemas/prepare-material-v1.json`](../backend/internal/generation/schemas/prepare-material-v1.json) | Ожидаемый JSON с единственным полем `level` |
| [`backend/internal/models/llm`](../backend/internal/models/llm) | Структуры `Material` и `LLMRequest` |
| [`backend/internal/database/invariants.sql`](../backend/internal/database/invariants.sql) | Дополнительные проверки статусов, связей и единственного активного запроса |

Основные действия названы по результату:

| Метод | Действие |
|---|---|
| `MaterialService.CreateTextMaterialAndQueuePreparation(ctx, key, input)` | Сохранить текст и запрос либо вернуть прежнюю пару |
| `MaterialService.GetMaterialByID(ctx, id)` | Прочитать материал без проверки владельца |
| `MaterialService.GetLLMRequestByID(ctx, id)` | Прочитать состояние запроса без проверки владельца |
| `ModuleGenerationService.CreateGeneratedModule(ctx, materialID, key, input)` | Создать модуль и запрос генерации либо вернуть прежнюю пару |
| `ModuleGenerationService.ListGeneratedModuleExercises(ctx, moduleID, limit, offset)` | Получить страницу упражнений готового модуля |
| `PreparationWorker.RunUntilStopped` | Выполнять очередь до остановки приложения |
| `PreparationWorker.ProcessNextPreparationRequest` | Обработать не больше одной работы |
| `ChatCompletionClient.HasAPIKeys` | Проверить наличие ключей без сетевого вызова |
| `ChatCompletionClient.DetectCEFRLevel` | Определить уровень текста |
| `ChatCompletionClient.GenerateExercises` | Запросить и проверить лексические и грамматические задания |
| `NormalizeAndValidateText` | Проверить и нормализовать текст |
| `ExtractTextFromFile` | Извлечь текст для предварительного просмотра |

`CEFRLevelDetector` и `ExerciseGenerator` связывают воркеры с клиентом модели. Детали HTTP находятся в `generation`. Общие настройки `LLMWorkerOptions` включают `LLMRequestTimeout`, `QueuePollInterval` и `MaxAttempts`. Решение о повторе принимает очередь.

## Что хранится в БД и передаётся наружу

В `materials` записываются технический владелец, ключ повторной отправки, название, необязательная тема, тип и полный нормализованный источник. При успехе добавляются `extracted_text` и CEFR. В `modules` хранятся ссылка на материал, тема, сложность, название и статус. В `exercises` хранятся упражнения, их порядок, ключи и объяснения.

В `llm_requests` сохраняются операция, статус, версия инструкции, провайдер и модель, число попыток, аренда, время создания и окончания, краткий итог либо безопасный код ошибки. Итог подготовки выглядит как `{"material_id":12,"level":"B1"}`, итог генерации — как `{"module_id":7,"exercise_count":4}`. Параметры генерации содержат число и типы упражнений.

Связи с `users` и составные внешние ключи между материалом и запросом остаются обязательными. Сейчас обе новые записи ссылаются на одного технического владельца. Он создаётся при необходимости внутри транзакции: email `shared-materials@lingoway.invalid`, имя «Общие материалы», роль `student`. Поле пароля содержит служебный маркер отключённого входа, а не пароль для авторизации. При откате создания материала создание этой учётной записи также откатывается.

Внутренние проверки владельца при записи результата сохраняют согласованность связей, но не разделяют доступ клиентов. Чтение по ID доступно и для материалов, которые были созданы до перехода на технического владельца. Они не переносятся на него автоматически: ключи прежних владельцев не входят в новое общее пространство повторной отправки.

Провайдер получает имя модели, системную инструкцию и полный нормализованный текст в отдельном пользовательском сообщении. При генерации в инструкцию добавляются целевой уровень и число упражнений каждого типа. Ключ API передаётся в заголовке запроса к провайдеру. Команды внутри материала считаются данными. Сервис проверяет структуру JSON, значения полей и связи между ID в ответе модели.

Новая инструкция оценивает лексику, грамматику и то, насколько читателю нужно понимать неявный смысл. В смешанном материале учитываются английские фрагменты. Для короткого образца модель выбирает ближайший обоснованный уровень. При полном отсутствии английского инструкция задаёт A1 как условное значение.

Ответ должен содержать ровно один ключ `level` со значением A1–C2. Код отклоняет дополнительные и повторные поля, другой регистр имени, второй JSON-объект, Markdown-обрамление, отказ модели и незавершённый ответ. Новые запросы получают версию `prepare-material-v2`. Воркер выбирает инструкцию по версии в записи очереди, поэтому запросы с `prepare-material-v1` продолжают обрабатываться по первой инструкции. Неизвестная версия завершается внутренним кодом `LLM_PROMPT_UNSUPPORTED`, а API показывает `PROCESSING_FAILED`. При обновлении нескольких экземпляров остановите воркеры старой версии до появления запросов v2.

Ключи остаются в окружении и памяти клиента. Необработанные ответы провайдера и произвольные тексты его ошибок не сохраняются и не выдаются API. При ошибке очереди журнал приложения получает общее сообщение без текста материала и ошибки драйвера. Настройки журнала БД скрывают значения SQL-параметров.

DTO не выдают `owner_user_id`, `source_text`, `object_key`, параметры очереди, аренду, provider/model и внутренний `result`. При этом готовый `extracted_text` намеренно возвращается по ID без проверки пользователя: текущие материалы общие, а сокрытие служебных полей не делает их приватными.

## Проверка сборки и ручной сценарий

Из каталога `backend` выполните:

```powershell
go build ./...
go vet ./...
```

Затем проверьте оба этапа на работающем сервере. [Сценарий в PowerShell](#пример-полного-сценария-в-powershell)

## Генерация лексики и грамматики

После подготовки материала отправьте `POST /api/materials/{material_id}/modules` с новым UUID в `Idempotency-Key`. Например, если первый POST вернул `material.id = 12`, отправьте запрос на `/api/materials/12/modules`.

| Поле JSON | Значение |
|---|---|
| `title` | Название модуля длиной 1–200 символов после удаления внешних пробелов |
| `topic_id` | Положительный ID существующей темы. Поле обязательно, даже если при создании материала тема была пропущена |
| `difficulty` | Желаемый уровень от A1 до C2. Для первой пробы подойдёт значение `level` из готового материала |
| `count` | Общее число упражнений от 2 до 30. Оно должно быть не меньше числа выбранных типов |
| `types` | Непустой список уникальных типов из таблицы ниже |

Каждому типу достаётся одинаковое число упражнений, остаток распределяется по одному первым типам в списке. Например, пять упражнений двух типов распределятся как три и два.

| Лексика | Грамматика |
|---|---|
| `vocabulary_gap` — пропуск слова | `grammar_brackets` — раскрытие скобок |
| `vocabulary_matching_translation` — подбор перевода | `grammar_tense` — выбор времени |
| `vocabulary_matching_definition` — подбор определения | `grammar_word_order` — порядок слов |
| `vocabulary_context_choice` — выбор слова по контексту | |

Если PostgreSQL запущен через Compose, выполните эти команды из корня проекта, чтобы посмотреть ID тем. Можно выбрать строку с `slug = it`.

```powershell
$dbUser = docker compose exec -T db printenv POSTGRES_USER
$dbName = docker compose exec -T db printenv POSTGRES_DB
docker compose exec -T db psql -U $dbUser -d $dbName -c 'SELECT id, slug, name FROM topics ORDER BY id'
```

Если используете свой PostgreSQL, выполните тот же SQL в подключённой базе.

Пример запроса с существующей темой `1` и уровнем `B1`.

```http
POST /api/materials/12/modules HTTP/1.1
Content-Type: application/json
Idempotency-Key: 70c66f36-5426-43e8-9e2a-04e8733f4a3f

{"title":"Algorithms practice","topic_id":1,"difficulty":"B1","count":4,"types":["vocabulary_gap","grammar_word_order"]}
```

`POST /api/materials/12/modules` вернёт HTTP 202. Ниже показаны поля, которые нужны для следующих шагов.

```json
{
  "module": {"id": 7, "material_id": 12, "status": "generating", "exercise_count": 0},
  "llm_request": {"id": 35, "operation": "generate_module", "module_id": 7, "status": "queued", "error": null}
}
```

Опрашивайте `GET /api/llm-requests/35` раз в 2–3 секунды до `succeeded` или `failed`. Воркер загружает подготовленный текст, вызывает модель с инструкцией `generate-module-v1` и проверяет формат, число заданий и ссылки в ключах. Упражнения, статус модуля `ready` и статус запроса `succeeded` сохраняются одной транзакцией. Если ответ модели не проходит проверку, модуль получает статус `failed`.

### Чтение упражнений

После успешной генерации прочитайте упражнения по ID модуля. Ответ содержит `items`, `limit`, `offset` и `has_more`. Элементы идут по возрастанию `position`. По умолчанию сервер возвращает до 20 элементов с `offset=0`. Допустимый `limit` — от 1 до 100, `offset` — от нуля.

```http
GET /api/modules/7/exercises?limit=1&offset=0 HTTP/1.1
```

```json
{
  "items": [
    {
      "id": 101,
      "module_id": 7,
      "position": 1,
      "type": "vocabulary_gap",
      "instruction": "Выберите слово для пропуска.",
      "question_text": "An ___ is a series of steps used to solve a problem.",
      "content": {
        "format": "single_choice",
        "options": [
          {"id": "a", "text": "screen"},
          {"id": "b", "text": "algorithm"},
          {"id": "c", "text": "keyboard"},
          {"id": "d", "text": "file"}
        ]
      },
      "level": "B1",
      "topic_id": 1
    }
  ],
  "limit": 1,
  "offset": 0,
  "has_more": true
}
```

Поскольку `has_more` равно `true`, для следующей страницы отправьте `GET /api/modules/7/exercises?limit=1&offset=1`. В API возвращаются поля для показа ученику. Ключи `answer_key` и объяснения `explanation` хранятся на сервере. [Шаблоны](exercise-templates.json) · [Примеры](exercise-examples.json)

## Пример полного сценария в PowerShell

Выполните блок целиком в одной сессии PowerShell после запуска сервера. Укажите ID темы из таблицы `topics`. Если переопределили `PORT`, укажите свой порт в `$baseUrl`. Для пробы с файлом замените `$materialText` на поле `text`, полученное при импорте. [Запуск](#запуск) · [Как посмотреть темы](#генерация-лексики-и-грамматики) · [Импорт файла](#предварительный-импорт-файла)

```powershell
function Send-LLMPost($Request) {
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        try {
            return Invoke-RestMethod @Request
        } catch {
            # При обрыве соединения повторяем тот же адрес, тело и UUID.
            if ($_.Exception.Response -or $attempt -eq 3) { throw }
            Start-Sleep -Seconds 2
        }
    }
}

function Wait-LLMRequest([long]$RequestId) {
    $statusUrl = "$baseUrl/api/llm-requests/$RequestId"
    $deadline = [DateTimeOffset]::UtcNow.AddMinutes(5)
    while ([DateTimeOffset]::UtcNow -lt $deadline) {
        $request = Invoke-RestMethod -Uri $statusUrl -TimeoutSec 10 -ErrorAction Stop
        if ($request.status -eq 'succeeded') { return $request }
        if ($request.status -eq 'failed') {
            throw "Запрос $RequestId завершился с ошибкой $($request.error.code). $($request.error.message)"
        }
        Start-Sleep -Seconds 2
    }
    throw "Истекло время ожидания. Текущий статус доступен по адресу $statusUrl"
}

$baseUrl = 'http://localhost:8080'
$topicId = [long](Read-Host 'Введите ID существующей темы')
if ($topicId -lt 1) { throw 'ID темы должен быть положительным числом.' }
$materialText = 'An algorithm is a series of steps used to solve a problem. A programmer tests each step before using the algorithm.'

$materialPost = @{
    Uri = "$baseUrl/api/materials"
    Method = 'Post'
    Headers = @{ 'Idempotency-Key' = [guid]::NewGuid().ToString() }
    ContentType = 'application/json; charset=utf-8'
    Body = (@{ kind = 'text'; title = 'Algorithms'; text = $materialText } | ConvertTo-Json)
    TimeoutSec = 15
    ErrorAction = 'Stop'
}
$created = Send-LLMPost $materialPost
Write-Host "Материал $($created.material.id), запрос подготовки $($created.llm_request.id)"
$null = Wait-LLMRequest $created.llm_request.id

$material = Invoke-RestMethod -Uri "$baseUrl/api/materials/$($created.material.id)" -TimeoutSec 10 -ErrorAction Stop
if ($material.status -ne 'ready') { throw 'Материал ещё не готов.' }
$material | Select-Object id, status, level, extracted_text

$modulePost = @{
    Uri = "$baseUrl/api/materials/$($material.id)/modules"
    Method = 'Post'
    Headers = @{ 'Idempotency-Key' = [guid]::NewGuid().ToString() }
    ContentType = 'application/json; charset=utf-8'
    Body = (@{
        title = 'Algorithms practice'
        topic_id = $topicId
        difficulty = $material.level
        count = 4
        types = @('vocabulary_gap', 'grammar_word_order')
    } | ConvertTo-Json)
    TimeoutSec = 15
    ErrorAction = 'Stop'
}
$generated = Send-LLMPost $modulePost
Write-Host "Модуль $($generated.module.id), запрос генерации $($generated.llm_request.id)"
$null = Wait-LLMRequest $generated.llm_request.id

$exerciseUrl = "$baseUrl/api/modules/$($generated.module.id)/exercises"
$offset = 0
do {
    $page = Invoke-RestMethod -Uri "${exerciseUrl}?limit=2&offset=$offset" -TimeoutSec 10 -ErrorAction Stop
    $page.items | ConvertTo-Json -Depth 10
    $offset += $page.limit
} while ($page.has_more)
```

Сценарий опрашивает каждый запрос раз в две секунды с пределом ожидания около пяти минут. Если ожидание истекло, продолжите опрос по тому же ID. Если POST завершился сетевой ошибкой и ответ так и не пришёл, повторите нужную строку `$created = Send-LLMPost $materialPost` или `$generated = Send-LLMPost $modulePost` в той же сессии и продолжите со следующей строки. Эти переменные сохраняют адрес, тело и UUID. Повтор всего блока создаст новые UUID.

## Как расширять модуль

- Новый текстовый формат добавляется в `text_import.go` с ограничениями чтения. Объёмный разбор можно вынести в отдельный файл, как для DOCX. Это остаётся предварительным импортом, пока не появится хранение бинарного источника.
- Другой протокол модели реализуется за `CEFRLevelDetector`. Ошибки сохраняют известные безопасные коды, решение о повторе остаётся у воркера.
- Изменение инструкции или результата требует согласовать промпт, схему, разбор ответа и `CEFRDetectionPromptVersion`. Версию существующей очереди нельзя молча подменять другой инструкцией.
- При возврате авторизации потребуется получать владельца из проверенного пользователя, ограничить чтение и вернуть отдельное пространство ключей для каждого пользователя. Технический владелец сам по себе этого не обеспечивает.
- URL, настоящие файловые источники и аудио требуют отдельного получения, хранения или распознавания исходника. Одного расширения списка `kind` недостаточно.

Входные тексты, промпт и ответы для ручной пробы генерации в Codex собраны отдельно. [Результаты проб](LLM_GENERATION_TRIALS.md)

При изменении поведения обновляйте этот документ и реализованные операции [OpenAPI](be3/openapi.json). Здесь находится единое текстовое описание модуля; [CONTRACTS.md](CONTRACTS.md) оставляет ссылку на него рядом с проектируемыми API остальных частей системы.
