import React, { useEffect, useRef, useState } from 'react';
import MaterialEditor from './MaterialEditor.jsx';
import ExerciseSettings, { defaultTypes } from './ExerciseSettings.jsx';
import Lesson from './Lesson.jsx';
import Icon from './Icon.jsx';
import { checkHealth, createMaterial, createModule, extractMaterialFile, getExercises, newRequestKey, waitForReady } from './api.js';

function Header({ view, setView, canOpenLesson }) {
  const [health, setHealth] = useState('checking');
  useEffect(() => {
    const request = new AbortController();
    async function update() {
      try {
        await checkHealth({ signal: request.signal });
        setHealth('online');
      } catch {
        if (!request.signal.aborted) setHealth('offline');
      }
    }
    update();
    const timer = setInterval(update, 30000);
    return () => { clearInterval(timer); request.abort(); };
  }, []);
  return <header className="topbar"><div className="topbar-inner">
    <button type="button" className="brand" aria-label="LingoWay — к материалу" onClick={() => setView('material')}>
      <span className="brand-icon" aria-hidden="true"><i/><i/></span>Lingo<span>Way</span>
    </button>
    <nav aria-label="Разделы практики">
      <button type="button" aria-current={view === 'material' ? 'page' : undefined} onClick={() => setView('material')}>Материал</button>
      <button type="button" aria-current={view === 'lesson' ? 'page' : undefined} disabled={!canOpenLesson} title={!canOpenLesson ? 'Сначала создайте упражнения по материалу' : undefined} onClick={() => setView('lesson')}>Упражнения</button>
    </nav>
    <span className={'connection ' + health} role="status"><i/>{({checking: 'Проверяем API', online: 'API подключён', offline: 'API недоступен'})[health]}</span>
  </div></header>;
}

function Introduction() {
  return <section className="intro" aria-labelledby="page-title">
    <div><h1 id="page-title">Практика по материалу</h1><p>Добавьте текст на английском и выберите, что хотите потренировать.</p></div>
    <div className="flashcards" aria-hidden="true">
      <div className="flash-back"><i/><i/><i/></div>
      <div className="flash-front"><strong>Aa</strong><i/><i/></div>
      <div className="pencil"><i/></div>
    </div>
  </section>;
}

function freshRequest(signature) {
  return { signature, key: newRequestKey(), url: null, ready: null };
}

export default function App() {
  const [view, setView] = useState('material');
  const [text, setText] = useState('');
  const [title, setTitle] = useState('');
  const [fileName, setFileName] = useState('');
  const [types, setTypes] = useState(defaultTypes);
  const [count, setCount] = useState(8);
  const [difficulty, setDifficulty] = useState('auto');
  const [topicId, setTopicId] = useState('1');
  const [activity, setActivity] = useState('');
  const [notice, setNotice] = useState(null);
  const [assessment, setAssessment] = useState(null);
  const [lesson, setLesson] = useState(null);
  const controller = useRef(null);
  const sourceRequest = useRef(null);
  const moduleRequest = useRef(null);
  const textRef = useRef(null);
  const focusImportedText = useRef(false);
  const noticeRef = useRef(null);

  useEffect(() => () => controller.current?.abort(), []);
  useEffect(() => {
    if (!activity && focusImportedText.current) {
      focusImportedText.current = false;
      textRef.current?.focus();
    }
  }, [activity]);
  useEffect(() => {
    if (notice?.kind === 'error') noticeRef.current?.scrollIntoView({ block: 'nearest' });
  }, [notice]);

  function switchView(next) {
    setView(next);
    window.scrollTo(0, 0);
  }
  function changeText(value) {
    setText(value);
    setNotice(null);
  }
  async function importFile(file) {
    if (controller.current) return;
    const request = new AbortController();
    controller.current = request;
    setActivity('file');
    setNotice(null);
    try {
      const result = await extractMaterialFile(file, { signal: request.signal });
      setText(result.text);
      setFileName(file.name);
      if (!title.trim()) setTitle(file.name.replace(/\.[^.]+$/, '').slice(0, 200));
      focusImportedText.current = true;
    } catch (error) {
      if (!request.signal.aborted) setNotice({kind: 'error', text: error.message, code: error.code});
    } finally {
      controller.current = null;
      setActivity('');
    }
  }

  async function generate(event) {
    event.preventDefault();
    if (controller.current) return;
    const sourceText = text.trim();
    let validation;
    if (!sourceText) validation = 'Добавьте английский текст или загрузите файл.';
    else if (Array.from(sourceText).length > 20000) validation = 'Сократите материал до 20 000 символов.';
    else if (!types.length) validation = 'Выберите хотя бы один тип упражнений.';
    else if (!Number.isInteger(count) || count < Math.max(2,types.length) || count > 30) validation = 'Выберите от 2 до 30 заданий, хотя бы по одному на каждый выбранный тип.';
    else if (!Number.isSafeInteger(Number(topicId)) || Number(topicId) < 1) validation = 'В дополнительных параметрах укажите положительный номер существующей темы.';
    if (validation) {
      setNotice({kind: 'error', text: validation});
      if (!sourceText) textRef.current?.focus();
      return;
    }
    const request = new AbortController();
    controller.current = request;
    setNotice(null);
    const sourceTitle = title.trim() || Array.from(sourceText.split('\n')[0]).slice(0, 64).join('');
    const materialPayload = {kind: 'text', title: sourceTitle, text: sourceText};
    const signature = JSON.stringify(materialPayload);
    let phase = 'material';
    try {
      setActivity('material');
      // Сохраняем ключ до отправки. При обрыве связи повторяем тот же POST, а если
      // получили Location — продолжаем чтение по нему без создания второй записи.
      if (sourceRequest.current?.signature !== signature) {
        sourceRequest.current = freshRequest(signature);
        moduleRequest.current = null;
      }
      const source = sourceRequest.current;
      source.url ??= await createMaterial(materialPayload, { key: source.key, signal: request.signal });
      source.ready ??= await waitForReady(source.url, { signal: request.signal });
      setAssessment({ text: sourceText, level: source.ready.level });
      phase = 'createModule';
      setActivity('module');
      const payload = {title: sourceTitle, topic_id: Number(topicId), difficulty: difficulty === 'auto' ? source.ready.level : difficulty, count, types};
      const moduleSignature = JSON.stringify([source.url, payload]);
      if (moduleRequest.current?.signature !== moduleSignature) moduleRequest.current = freshRequest(moduleSignature);
      const output = moduleRequest.current;
      output.url ??= await createModule(source.url, payload, { key: output.key, signal: request.signal });
      phase = 'readModule';
      output.ready ??= await waitForReady(output.url, { signal: request.signal });
      setActivity('exercises');
      const exercises = await getExercises(output.url, { signal: request.signal });
      if (exercises.length !== count) throw new Error('Список упражнений получен не полностью. Повторите загрузку.');
      setLesson({module: output.ready, exercises});
      // Следующий явный запуск создаст другой набор по тому же готовому материалу.
      moduleRequest.current = null;
      switchView('lesson');
    } catch (error) {
      if (request.signal.aborted) {
        setNotice({kind: 'info', text: 'Ожидание остановлено. Можно продолжить с того же места.', resume: true});
      } else {
        if (error.terminal || error.code === 'NOT_FOUND') {
          if (phase === 'material') sourceRequest.current = null;
          moduleRequest.current = null;
        }
        const missingTopic = phase === 'createModule' && error.code === 'NOT_FOUND';
        const message = missingTopic ? 'Сервер не нашёл тему или исходный материал. Проверьте ID темы в дополнительных параметрах.' : error.message;
        // После исправления темы перечитываем материал по прежнему адресу.
        if (missingTopic) sourceRequest.current.ready = null;
        setNotice({kind: 'error', text: message, code: error.code, resume: !error.terminal && !missingTopic});
      }
    } finally {
      controller.current = null;
      setActivity('');
    }
  }

  const generating = Boolean(activity && activity !== 'file');
  const progressText = {
    material: 'Готовим материал и определяем уровень CEFR…',
    module: 'Материал готов. Модель составляет упражнения…',
    exercises: 'Загружаем упражнения готового модуля…',
  }[activity];
  return <>
    <Header view={view} setView={switchView} canOpenLesson={Boolean(lesson && !activity)}/>
    <main className="workspace">
      {view === 'material' && <>
        <Introduction/>
        {generating && <div className="feedback working" role="status"><i className="spinner"/><span>{progressText}</span><button type="button" onClick={() => controller.current?.abort()}>Остановить ожидание</button></div>}
        {notice && <div ref={noticeRef} className={'feedback ' + notice.kind} role={notice.kind === 'error' ? 'alert' : 'status'}>
          <Icon name={notice.kind === 'error' ? 'alert' : 'chat'}/><span>{notice.text}{notice.code && <small>{notice.code}</small>}</span>
        </div>}
        <form className="material-layout" onSubmit={generate} noValidate>
          <MaterialEditor title={title} setTitle={setTitle} text={text} setText={changeText} disabled={Boolean(activity)} uploading={activity === 'file'} fileName={fileName} textRef={textRef} onFile={importFile}
            detectedLevel={assessment?.text === text.trim() ? assessment.level : null}/>
          <ExerciseSettings types={types} onTypes={setTypes} count={count} setCount={setCount} difficulty={difficulty} setDifficulty={setDifficulty} topicId={topicId} setTopicId={setTopicId} disabled={Boolean(activity)} busy={generating}
            actionLabel={notice?.resume ? 'Продолжить генерацию' : 'Создать упражнения'}/>
        </form>
      </>}
      {lesson && <div hidden={view !== 'lesson'}><Lesson {...lesson} onBack={() => switchView('material')}/></div>}
    </main>
  </>;
}
