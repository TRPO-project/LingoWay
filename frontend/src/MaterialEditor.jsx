import React, { useRef, useState } from 'react';
import Icon from './Icon.jsx';
import { exampleText } from './exerciseTypes.js';

export default function MaterialEditor({ title, setTitle, text, setText, disabled, onFile, fileName, uploading, textRef, detectedLevel }) {
  const [mode, setMode] = useState('text');
  const [dragging, setDragging] = useState(false);
  const picker = useRef(null);
  const length = Array.from(text).length;

  function pickFile(file) {
    if (!disabled && file) onFile(file);
  }

  return <section className="panel editor-panel" aria-labelledby="material-heading">
    <div className="panel-heading"><span className="heading-icon"><Icon name="file" size={21}/></span><h2 id="material-heading">Учебный материал</h2></div>
    <div className="panel-body">
      <fieldset disabled={disabled}>
        <div className="source-modes" role="group" aria-label="Способ добавления материала">
          <button type="button" aria-pressed={mode === 'text'} onClick={() => setMode('text')}><Icon name="text" size={16}/> Ввести текст</button>
          <button type="button" aria-pressed={mode === 'file'} onClick={() => setMode('file')}><Icon name="upload" size={16}/> Загрузить файл</button>
        </div>
        {mode === 'file' && <div className={'drop-area' + (dragging ? ' dragging' : '')}
          onDragOver={e => { e.preventDefault(); if (!disabled) setDragging(true); }}
          onDragLeave={() => setDragging(false)}
          onDrop={e => {e.preventDefault(); setDragging(false); pickFile(e.dataTransfer.files[0]);}}>
          <Icon name="upload" size={22}/>
          <button type="button" onClick={() => picker.current.click()}>{uploading ? 'Извлекаем текст…' : (fileName || 'Выбрать файл')}</button>
          <small>Или перетащите сюда TXT, MD или DOCX до 2 МиБ</small>
          <input ref={picker} type="file" accept=".txt,.md,.docx" hidden aria-label="Файл материала" onChange={e => {pickFile(e.target.files[0]); e.target.value = '';}} />
        </div>}
        <div className="label-line"><label htmlFor="material-title">Название</label><span>Необязательно</span></div>
        <input id="material-title" className="text-input" value={title} maxLength={200} placeholder="Например, At the library" onChange={e => setTitle(e.target.value)} />
        <div className="label-line"><label htmlFor="material-text">Текст на английском</label>{detectedLevel && <span>Уровень материала · {detectedLevel}</span>}</div>
        <div className="writing-area">
          <textarea ref={textRef} id="material-text" lang="en" value={text} onChange={e => setText(e.target.value)}
            placeholder="Вставьте статью, отрывок из книги или свои заметки…" aria-describedby="character-count" />
          <div className="writing-tools">
            {!text.trim() && <button className="example-link" type="button" onClick={() => {setText(exampleText); textRef.current.focus();}}>
              <Icon name="paste" size={15}/> Вставить пример
            </button>}
            <span id="character-count" className={length > 20000 ? 'over-limit' : ''}>{length.toLocaleString('ru-RU')} / 20 000</span>
          </div>
        </div>
      </fieldset>
    </div>
  </section>;
}
