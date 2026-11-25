// Клиент обращается к API на том же адресе, с которого открыли приложение.
// Адрес материала или модуля выдаёт сервер в заголовке Location.
const materialPath = /^\/api\/materials\/[1-9]\d*$/;
const modulePath = /^\/api\/modules\/[1-9]\d*$/;
const pollIntervalMs = 2000;
const pollTimeoutMs = 5 * 60 * 1000;

function apiError(code, message) {
  return Object.assign(new Error(message), { code });
}

function invalidResponse() {
  return apiError('INVALID_API_RESPONSE', 'Сервер вернул неожиданный ответ. Проверьте подключение к API LingoWay.');
}

export function newRequestKey() {
  if (crypto.randomUUID) return crypto.randomUUID();
  // getRandomValues доступен и при открытии локального стенда по HTTP в сети.
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
  return [hex.slice(0, 8), hex.slice(8, 12), hex.slice(12, 16), hex.slice(16, 20), hex.slice(20)].join('-');
}

async function requestJSON(path, options = {}) {
  const timeout = AbortSignal.timeout(15000);
  const signal = options.signal ? AbortSignal.any([options.signal, timeout]) : timeout;
  try {
    const response = await fetch(path, { ...options, signal, cache: 'no-store', mode: 'same-origin' });
    const body = await response.text();
    let data;
    try {
      data = JSON.parse(body);
    } catch {
      if (response.ok) throw invalidResponse();
    }
    if (!response.ok) {
      throw apiError(data?.error?.code || `HTTP_${response.status}`,
        data?.error?.message || `Сервис недоступен (HTTP ${response.status}).`);
    }
    return { data, response };
  } catch (error) {
    if (options.signal?.aborted) throw options.signal.reason;
    if (timeout.aborted) throw apiError('REQUEST_TIMEOUT', 'Сервер не ответил вовремя. Попробуйте ещё раз.');
    if (error instanceof TypeError) throw apiError('NETWORK_ERROR', 'Не удалось связаться с сервером. Проверьте подключение.');
    throw error;
  }
}

export async function checkHealth({ signal } = {}) {
  const { data } = await requestJSON('/api/health', { signal });
  if (data?.status !== 'ok') throw invalidResponse();
  return data;
}

export async function extractMaterialFile(file, { signal } = {}) {
  if (!file || !/\.(txt|md|docx)$/i.test(file.name)) {
    throw apiError('UNSUPPORTED_FILE_FORMAT', 'Выберите файл TXT, MD или DOCX.');
  }
  if (file.size > 2 * 1024 * 1024) {
    throw apiError('MATERIAL_TOO_LARGE', 'Размер файла должен быть не более 2 МиБ.');
  }
  const body = new FormData();
  body.append('file', file);
  const { data } = await requestJSON('/api/materials/extract', { method: 'POST', body, signal });
  if (typeof data?.text !== 'string' || !data.text.trim()) throw invalidResponse();
  return data;
}

async function createResource(path, payload, expectedPath, { key, signal }) {
  if (!key) throw apiError('MISSING_REQUEST_KEY', 'Для создания ресурса нужен ключ запроса.');
  const options = {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
    body: JSON.stringify(payload),
    signal,
  };
  // Если ответ потерялся, первый POST уже мог создать запись. Один повтор с тем
  // же телом и UUID получает адрес этой записи; новую задачу сервер не создаст.
  let result;
  try {
    result = await requestJSON(path, options);
  } catch (error) {
    if (error.code !== 'NETWORK_ERROR' && error.code !== 'REQUEST_TIMEOUT') throw error;
    result = await requestJSON(path, options);
  }
  const location = result.response.headers.get('Location');
  if (result.response.status !== 202 || !expectedPath.test(location)) throw invalidResponse();
  return location;
}

export function createMaterial(payload, options) {
  return createResource('/api/materials', payload, materialPath, options);
}

export function createModule(materialUrl, payload, options) {
  if (!materialPath.test(materialUrl)) throw invalidResponse();
  return createResource(`${materialUrl}/modules`, payload, modulePath, options);
}

function waitBetweenReads(signal) {
  return new Promise((resolve, reject) => {
    signal.throwIfAborted();
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', cancel);
      resolve();
    }, pollIntervalMs);
    function cancel() {
      clearTimeout(timer);
      reject(signal.reason);
    }
    signal.addEventListener('abort', cancel, { once: true });
  });
}

// onUpdate получает ту же карточку, которую вернул GET, включая финальное состояние.
// Остановка ожидания в интерфейсе не отменяет уже поставленную работу на сервере.
export async function waitForReady(resourceUrl, { signal, onUpdate } = {}) {
  if (!materialPath.test(resourceUrl) && !modulePath.test(resourceUrl)) throw invalidResponse();
  const deadline = AbortSignal.timeout(pollTimeoutMs);
  const pollingSignal = signal ? AbortSignal.any([signal, deadline]) : deadline;
  try {
    while (true) {
      const { data } = await requestJSON(resourceUrl, { signal: pollingSignal });
      onUpdate?.(data);
      if (data?.status === 'ready') return data;
      if (data?.status === 'failed') {
        const error = apiError(data.error?.code || 'PROCESSING_FAILED',
          data.error?.message || 'Обработка завершилась ошибкой.');
        error.terminal = true;
        throw error;
      }
      if (data?.status !== 'pending' && data?.status !== 'generating') throw invalidResponse();
      await waitBetweenReads(pollingSignal);
    }
  } catch (error) {
    if (signal?.aborted) throw signal.reason;
    if (deadline.aborted) throw apiError('POLL_TIMEOUT', 'Обработка ещё идёт. Продолжите ожидание, чтобы получить результат.');
    throw error;
  }
}

export async function getExercises(moduleUrl, { signal } = {}) {
  if (!modulePath.test(moduleUrl)) throw invalidResponse();
  const exercises = [];
  const limit = 20;
  while (true) {
    const { data } = await requestJSON(`${moduleUrl}/exercises?limit=${limit}&offset=${exercises.length}`, { signal });
    if (!Array.isArray(data?.items) || data.offset !== exercises.length || typeof data.has_more !== 'boolean') {
      throw invalidResponse();
    }
    exercises.push(...data.items);
    if (!data.has_more) return exercises;
    if (!data.items.length) throw invalidResponse();
  }
}
