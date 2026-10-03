## POST /api/login пример
Вход пользователя.

**Запрос:**
```json
{
  "email": "user@mail.com",
  "password": "12345"
}
```

**Ответ (успех):**
```json
{
  "token": "eyJhbGci...",
  "user": { "id": 1, "name": "Иван", "level": "B1" }
}
```

**Ответ (ошибка):**
```json
{
  "error": "Неверный email или пароль"
}
```