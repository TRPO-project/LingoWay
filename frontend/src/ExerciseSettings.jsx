import React from 'react';
import Icon from './Icon.jsx';
import { exerciseGroups } from './exerciseTypes.js';

const levels = ['A1', 'A2', 'B1', 'B2', 'C1', 'C2'];
export const defaultTypes = [...exerciseGroups[0].types, ...exerciseGroups[2].types].map(([id]) => id);

export default function ExerciseSettings({types, onTypes, count, setCount, difficulty, setDifficulty, topicId, setTopicId, disabled, busy, actionLabel}) {
  const minCount = Math.max(2, types.length);
  function toggle(ids) {
    const allSelected = ids.every(id => types.includes(id));
    // Порядок типов определяет, кому сервер отдаст остаток при делении количества.
    const next = exerciseGroups.flatMap(group => group.types.map(([id]) => id)).filter(id =>
      ids.includes(id) ? !allSelected : types.includes(id));
    onTypes(next);
    if (count < next.length) setCount(next.length);
  }

  return <section className="panel settings-panel" aria-labelledby="settings-heading">
    <div className="panel-heading"><span className="heading-icon"><Icon name="sliders" size={21}/></span><h2 id="settings-heading">Настройки практики</h2></div>
    <div className="panel-body">
      <fieldset disabled={disabled}>
        <div className="label-line first"><span className="label">Типы упражнений</span></div>
        <div className="group-grid" role="group" aria-label="Типы упражнений">
          {exerciseGroups.map(group => {
            const ids = group.types.map(([id]) => id);
            const active = ids.filter(id => types.includes(id)).length;
            return <button key={group.name} type="button" className={'group-chip' + (active ? ' selected' : '')}
              aria-pressed={active === 0 ? false : (active === ids.length ? true : 'mixed')} onClick={() => toggle(ids)}>
              <Icon name={group.icon} size={17}/><span>{group.name}</span>
              <i className="tick">{active > 0 && <Icon name={active === ids.length ? 'check' : 'minus'} size={11}/>}</i>
            </button>;
          })}
        </div>
        <div className="setting-row">
          <label htmlFor="exercise-count">Количество заданий</label>
          <div className="stepper">
            <button type="button" disabled={count <= minCount} aria-label="Уменьшить количество" onClick={() => setCount(count-1)}><Icon name="minus" size={14}/></button>
            <input id="exercise-count" type="number" min={minCount} max="30" value={count} onChange={e => {
              const raw = e.target.value;
              setCount(raw === '' ? '' : Number(raw));
            }} onBlur={() => setCount(Math.max(minCount, Math.min(30, Math.trunc(Number(count) || minCount))))}/>
            <button type="button" disabled={count >= 30} aria-label="Увеличить количество" onClick={() => setCount(Math.min(30,Math.max(minCount,Number(count)+1)))}><Icon name="plus" size={14}/></button>
          </div>
        </div>
        <div className="setting-row">
          <label htmlFor="difficulty">Уровень CEFR</label>
          <select id="difficulty" value={difficulty} onChange={e => setDifficulty(e.target.value)}>
            <option value="auto">По материалу</option>
            {levels.map(level => <option key={level}>{level}</option>)}
          </select>
        </div>
        <details className="advanced">
          <summary><Icon name="chevron" size={15}/> Дополнительные параметры</summary>
          <label className="label-line" htmlFor="topic-id">ID темы в базе</label>
          <input id="topic-id" className="text-input" type="number" value={topicId} min="1" step="1" onChange={e => setTopicId(e.target.value)} />
          <small>В новой базе тема 1 — IT. Для другой темы укажите её действующий номер.</small>
          <div className="type-list" role="group" aria-label="Точные типы упражнений">
            {exerciseGroups.flatMap(group => group.types).map(([id,label]) => <label key={id}><input type="checkbox" checked={types.includes(id)} onChange={() => toggle([id])} />{label}</label>)}
          </div>
        </details>
      </fieldset>
      <button type="submit" className="primary generate" disabled={disabled}>
        <Icon name="spark" size={18}/>{busy ? 'Генерация…' : actionLabel}<Icon name="arrow" size={17}/>
      </button>
    </div>
  </section>;
}
