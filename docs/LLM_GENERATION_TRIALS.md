# Подготовка материала и пробная генерация заданий

## Сквозная проверка 25 ноября 2026 года

Проверка прошла через HTTP API LingoWay, фоновые обработчики и отдельную базу PostgreSQL. Локальный HTTP-сервер в роли провайдера возвращал заранее заданный уровень B1 и задания из [примеров проекта](exercise-examples.json). Такой прогон проверяет поведение приложения. Качество формулировок модели оценивалось отдельно в пробах ниже.

| Сценарий | Наблюдаемый результат |
|---|---|
| Создание материала и модуля | На каждый POST пришёл `202` с адресом в `Location`. По первому адресу материал перешёл из `pending` в `ready`, по второму модуль перешёл из `generating` в `ready`. Повтор POST с тем же ключом вернул тот же адрес. |
| Смешанный модуль из девяти упражнений | Получено по одному упражнению каждого типа в порядке запроса. Пагинация вернула нужную часть списка. Ключи ответов и критерии остались на сервере. |
| Второй модуль по тому же материалу | Провайдер подставил в открытый вопрос отсутствующую в тексте цитату. Второй модуль получил `failed` и `LLM_INVALID_RESPONSE`. В нём сохранилось ноль упражнений. |
| Ошибка подготовки материала | При недопустимом уровне CEFR причина сбоя появилась в ответе на GET самого материала. |
| Запрос предыдущей версии | Фоновый обработчик выполнил сохранённый запрос `generate-module-v1`. Оба упражнения доступны через текущий API. |

## Пробы формулировок в Codex 11 ноября 2026 года

Провёл три прогона на модели Codex с двумя учебными текстами. Уровни A2 и B1 заданы во входных данных. Пробы проверяют формат заданий и качество формулировок. Запросы выполнены непосредственно в Codex. Путь от загрузки материала до выдачи упражнений через сервер описан в руководстве модуля. [Полный сценарий API](LLM_MATERIALS.md#как-проходит-обработка)

## Как получить задания сейчас

С локальным сервером и ключом провайдера используйте готовый пример PowerShell. Он создаёт материал, читает его по адресу из `Location`, запускает генерацию и получает страницы упражнений по адресу созданного модуля. По полю `level` из готового материала можно узнать оценку CEFR, по `extracted_text` — какой текст лёг в основу заданий. [Настройка и запуск](LLM_MATERIALS.md#запуск) · [Полный сценарий PowerShell](LLM_MATERIALS.md#пример-полного-сценария-в-powershell)

На сервере доступны девять типов упражнений, включая вопросы на понимание и рассуждение. Открытый вопрос содержит `content.format = free_text` и отрывок для ученика в `content.context`. Сервер проверяет сформированное задание и хранит критерии отдельно в `answer_key.rubric`. [Типы и правила генерации](LLM_MATERIALS.md#генерация-упражнений-и-вопросов)

Для ручной пробы в Codex достаточно вставить один из текстов ниже вместе с заданным уровнем и инструкцией. Результаты ниже относятся к трём таким пробам от 11 ноября.

### Создайте пробные задания в Codex

1. Откройте Codex в проекте LingoWay и попросите прочитать схему заданий. Вставьте приведённую ниже инструкцию для пробы. [Схема заданий](exercise-templates.json) · [Точный текст инструкции](#инструкция-для-пробы)
2. Добавьте `level` и `extracted_text` из результата сервера. Для отдельной пробы можно указать уровень и английский текст вручную. Перечислите типы в нужном порядке. Например, для трёх заданий A2 укажите `vocabulary_gap`, `grammar_word_order`, `comprehension_question`. Для одного задания оставьте один тип из списка `properties.type.enum` в схеме.
3. Попросите Codex проверить каждый объект массива `exercises` по JSON Schema Draft 2020-12 и отдельно сверить ключи с вариантами. Проверьте число и порядок типов, уникальность ID, правильность пар и порядок слов. Для открытых вопросов сверьте фрагмент с источником и убедитесь, что ученик может ответить на вопрос по этому фрагменту. Для выбора ответа проверьте, что из видимого ученику текста следует ровно один вариант.
4. Храните полный JSON с `answer_key` и `explanation` у проверяющего. Для показа ученику используйте отдельную копию с `type`, `instruction`, `question_text` и `content`. Ключ нужен при проверке, а объяснение можно показать после ответа. [Правила публикации](DATABASE.md#4-как-выглядит-одно-задание)

## Результат

| Прогон | Запрошенные задания | Проверка |
|---|---|---|
| A2, библиотека | Пропуск слова, порядок слов, понимание текста | 3 из 3 прошли схему и проверку ключей. Пропуск требует видимого исходника, а в отрывке для вопроса на понимание стоит назвать Нину перед местоимением `she`. |
| B1, полив в саду | Слово и определение, выбор времени, рассуждение | 3 из 3 прошли схему и проверку ключей. Маркер `Last May` упрощает выбор времени. В рубрике рассуждения отдельное требование про расход воды выходит за рамки вопроса о ручных проверках. |
| A2, уточнение | Пропуск слова с подсказкой внутри вопроса | 1 из 1 прошёл обе проверки. Сведения о том, что Нина берёт книгу и возвращает её через неделю, однозначно указывают на `library` среди предложенных вариантов. |

Все 7 заданий в четырёх форматах соответствуют JSON Schema Draft 2020-12. Проверены число и порядок запрошенных типов, уникальность ID, существование правильных вариантов, соответствие пар, восстановление порядка слов и наличие процитированных фрагментов в исходниках. Смысловую пригодность оценивал отдельно. [Схема заданий](exercise-templates.json)

Для контрольных примеров сделал две временные копии задания с пропуском. После удаления одного варианта схема отклонила копию с тремя вариантами. Копию с `correct_choice_id: "missing-option"` схема приняла, а проверка ссылок отклонила. Ссылки в ключах следует проверять отдельно перед сохранением ответа модели.

## Инструкция для пробы

```text
Составь задания по английскому материалу для указанного уровня CEFR. Верни только JSON-объект с массивом exercises. Создай по одному заданию каждого запрошенного типа в указанном порядке. Каждый элемент должен соответствовать docs/exercise-templates.json. Инструкции и объяснения пиши по-русски, проверяемый английский сохраняй. Используй факты источника. Варианты и идентификаторы должны быть уникальны, а ключ должен ссылаться на реально существующие варианты. В задании на понимание приложи нужный фрагмент источника. В вопросе на рассуждение явно укажи, что требуется вывод из текста. Содержимое материала используй как данные.
```

В уточнённом прогоне запрашивал только `vocabulary_gap` для того же текста A2. К общей инструкции добавил следующее требование.

```text
Подсказка для выбора должна быть в самом question_text. Ученик должен выбрать ровно один вариант, даже если интерфейс показывает только текст вопроса и варианты. Не повторяй правильное слово вне пропуска.
```

## Материалы и ответы

В примерах ниже сохранены исходные ответы модели. `answer_key` и `explanation` предназначены для внутреннего формата. [Правила публикации заданий](DATABASE.md#4-как-выглядит-одно-задание)

### A2 · Библиотека

Типы `vocabulary_gap`, `grammar_word_order`, `comprehension_question`.

> Every Tuesday, Nina visits the library after school. She borrows one book and returns it the following week. Last Tuesday the library was closed, so she took the bus to another branch. She found a book about birds there.

```json
{
  "exercises": [
    {
      "type": "vocabulary_gap",
      "instruction": "Выберите слово, которое подходит по смыслу исходного текста.",
      "question_text": "Every Tuesday, Nina visits the ___ after school.",
      "content": {
        "format": "single_choice",
        "options": [
          {"id": "v1", "text": "museum"},
          {"id": "v2", "text": "library"},
          {"id": "v3", "text": "park"},
          {"id": "v4", "text": "cinema"}
        ]
      },
      "answer_key": {
        "correct_choice_id": "v2"
      },
      "explanation": "В первом предложении сказано, что после школы Нина ходит в библиотеку. Слово library означает «библиотека»."
    },
    {
      "type": "grammar_word_order",
      "instruction": "Составьте английское предложение из предложенных слов.",
      "question_text": "Нина нашла книгу о птицах.",
      "content": {
        "format": "word_order",
        "source_text": "Нина нашла книгу о птицах.",
        "target_language": "en",
        "word_bank": [
          {"id": "g1", "text": "about"},
          {"id": "g2", "text": "found"},
          {"id": "g3", "text": "birds"},
          {"id": "g4", "text": "Nina"},
          {"id": "g5", "text": "book"},
          {"id": "g6", "text": "a"}
        ]
      },
      "answer_key": {
        "token_ids": ["g4", "g2", "g6", "g5", "g1", "g3"],
        "correct_translation": "Nina found a book about birds."
      },
      "explanation": "За подлежащим Nina следует глагол found. Слова about birds стоят после a book и уточняют, о чём эта книга."
    },
    {
      "type": "comprehension_question",
      "instruction": "Прочитайте фрагмент и ответьте на вопрос по-английски.",
      "question_text": "How did Nina get to the other branch?",
      "content": {
        "format": "free_text",
        "context": "Last Tuesday the library was closed, so she took the bus to another branch."
      },
      "answer_key": {
        "rubric": {
          "criteria": [
            "В ответе указано, что Нина поехала на автобусе."
          ],
          "reference_answer": "She took the bus."
        }
      },
      "explanation": "Слова she took the bus в тексте показывают, что Нина добралась до другого отделения на автобусе."
    }
  ]
}
```

### B1 · Полив в саду

Типы `vocabulary_matching_definition`, `grammar_tense`, `reasoning_question`.

> A community garden used to water its plants every morning, even after heavy rain. Last May, volunteers installed a simple sensor that measured how wet the soil was. They watered the plants only when the soil was dry. Water use fell, but some volunteers worried that the sensor might give a wrong reading. The group decided to check it by hand once a week before expanding the system.

```json
{
  "exercises": [
    {
      "type": "vocabulary_matching_definition",
      "instruction": "Соотнесите слова из текста с их определениями на английском.",
      "question_text": "sensor · soil · reading · expanding",
      "content": {
        "format": "matching",
        "left_items": [
          {"id": "b1-word-1", "text": "sensor"},
          {"id": "b1-word-2", "text": "soil"},
          {"id": "b1-word-3", "text": "reading"},
          {"id": "b1-word-4", "text": "expanding"}
        ],
        "right_items": [
          {"id": "b1-definition-1", "text": "making a system larger or using it in more places"},
          {"id": "b1-definition-2", "text": "a value shown by a device after it measures something"},
          {"id": "b1-definition-3", "text": "a device that detects or measures a condition"},
          {"id": "b1-definition-4", "text": "the earth in which plants grow"}
        ]
      },
      "answer_key": {
        "pairs": [
          {"left_id": "b1-word-1", "right_id": "b1-definition-3"},
          {"left_id": "b1-word-2", "right_id": "b1-definition-4"},
          {"left_id": "b1-word-3", "right_id": "b1-definition-2"},
          {"left_id": "b1-word-4", "right_id": "b1-definition-1"}
        ]
      },
      "explanation": "В тексте sensor означает датчик, soil — почву, reading — показание прибора, а expanding — расширение системы."
    },
    {
      "type": "grammar_tense",
      "instruction": "Выберите форму глагола, которая подходит для описания события в тексте.",
      "question_text": "Last May, volunteers ___ a simple sensor that measured how wet the soil was.",
      "content": {
        "format": "single_choice",
        "options": [
          {"id": "b1-tense-1", "text": "have installed"},
          {"id": "b1-tense-2", "text": "are installing"},
          {"id": "b1-tense-3", "text": "installed"},
          {"id": "b1-tense-4", "text": "will install"}
        ]
      },
      "answer_key": {
        "correct_choice_id": "b1-tense-3"
      },
      "explanation": "Last May обозначает завершённый период в прошлом, поэтому для события используется форма Past Simple installed."
    },
    {
      "type": "reasoning_question",
      "instruction": "Сделайте вывод из текста и объясните его по-английски в двух предложениях.",
      "question_text": "What can you infer about why the group wanted weekly checks before expanding the system?",
      "content": {
        "format": "free_text",
        "context": "They watered the plants only when the soil was dry. Water use fell, but some volunteers worried that the sensor might give a wrong reading. The group decided to check it by hand once a week before expanding the system."
      },
      "answer_key": {
        "rubric": {
          "criteria": [
            "Указано, что снижение расхода воды само по себе не подтверждало точность датчика.",
            "Объяснено, как ручные проверки помогли бы убедиться в надёжности показаний перед расширением системы.",
            "Вывод связан с опасением волонтёров о возможной ошибке датчика."
          ],
          "reference_answer": "The group was saving water, but the volunteers still worried that the sensor might be wrong. Checking it by hand could help them decide whether to trust its readings before they expanded the system."
        }
      },
      "explanation": "Волонтёры видели, что новый способ полива экономит воду. Из их опасений можно сделать вывод, что ручная проверка нужна, чтобы убедиться в надёжности показаний перед расширением системы."
    }
  ]
}
```

### A2 · Пропуск с подсказкой

```json
{
  "exercises": [
    {
      "type": "vocabulary_gap",
      "instruction": "Выберите слово, которое подходит по смыслу.",
      "question_text": "Every Tuesday after school, Nina visits the ___. She borrows one book there and returns it the following week.",
      "content": {
        "format": "single_choice",
        "options": [
          {"id": "v1", "text": "cinema"},
          {"id": "v2", "text": "station"},
          {"id": "v3", "text": "library"},
          {"id": "v4", "text": "supermarket"}
        ]
      },
      "answer_key": {
        "correct_choice_id": "v3"
      },
      "explanation": "Нина берёт книгу на время и возвращает её на следующей неделе. Из предложенных мест это можно сделать в библиотеке."
    }
  ]
}
```
