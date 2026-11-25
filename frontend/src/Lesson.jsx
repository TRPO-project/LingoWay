import { useEffect, useId, useRef, useState } from 'react';
import { exerciseGroups } from './exerciseTypes.js';

const exerciseNames = new Map(exerciseGroups.flatMap(group => group.types));

// Счётчик показывает, сколько форм заполнено. Ключи правильных ответов остаются на сервере.
function hasAnswer(exercise, answer) {
  if (answer === undefined) return false;
  switch (exercise.content.format) {
    case 'single_choice': return Boolean(answer);
    case 'matching': return exercise.content.left_items.every((_, index) => Boolean(answer[index]));
    case 'word_order': return answer.length >= 2;
    case 'free_text': return answer.trim().length > 0;
    default: return false;
  }
}

function AnswerForm({ exercise, answer, onChange, questionId }) {
  const content = exercise.content;
  switch (content.format) {
    case 'single_choice':
      return (
        <div className="lesson-choices" role="radiogroup" aria-labelledby={questionId}>
          {content.options.map(option => (
            <label key={option.id} className="lesson-choice">
              <input type="radio" name={questionId} value={option.id}
                checked={answer === option.id} onChange={() => onChange(option.id)} />
              <span>{option.text}</span>
            </label>
          ))}
        </div>
      );
    case 'matching': {
      const pairs = answer ?? [];
      return (
        <div className="lesson-matches">
          {content.left_items.map((left, index) => (
            <label key={left.id} className="lesson-match">
              <span>{left.text}</span>
              <select value={pairs[index] ?? ''} onChange={event => {
                const nextPairs = [...pairs];
                nextPairs[index] = event.target.value;
                onChange(nextPairs);
              }}>
                <option value="">Выбрать пару</option>
                {content.right_items.map(right => (
                  <option key={right.id} value={right.id}
                    disabled={pairs.includes(right.id) && pairs[index] !== right.id}>
                    {right.text}
                  </option>
                ))}
              </select>
            </label>
          ))}
        </div>
      );
    }
    case 'word_order': {
      // Два одинаковых слова могут быть разными токенами. Для выбора важен ID, а не текст.
      const sequence = answer ?? [];
      return (
        <>
          {content.source_text !== exercise.question_text && <p>{content.source_text}</p>}
          <p className="lesson-instruction">Выбирайте слова по порядку. Нажмите на слово в ответе, чтобы убрать его.</p>
          <div className="lesson-sequence" role="group" aria-label="Ваше предложение">
            {sequence.length === 0 && <span className="lesson-placeholder">Здесь появится ваше предложение</span>}
            {sequence.map(id => {
              const token = content.word_bank.find(item => item.id === id);
              return (
                <button type="button" key={id} className="lesson-token" aria-label={`Убрать «${token.text}»`}
                  onClick={() => onChange(sequence.filter(selected => selected !== id))}>
                  {token.text}
                </button>
              );
            })}
          </div>
          <div className="lesson-word-bank" role="group" aria-label="Слова для предложения">
            {content.word_bank.map(token => (
              <button type="button" key={token.id} className="lesson-token" disabled={sequence.includes(token.id)}
                onClick={() => onChange([...sequence, token.id])}>
                {token.text}
              </button>
            ))}
          </div>
        </>
      );
    }
    case 'free_text':
      return (
        <>
          <blockquote className="lesson-context">{content.context}</blockquote>
          {content.vocabulary?.length > 0 && (
            <p className="lesson-vocabulary">Слова для ответа: {content.vocabulary.join(' · ')}</p>
          )}
          <label>
            <span className="lesson-answer-label">Ваш ответ</span>
            <textarea className="lesson-answer" rows={4} lang="en" value={answer ?? ''}
              maxLength={10000} placeholder="Напишите ответ на английском"
              onChange={event => onChange(event.target.value)} />
          </label>
        </>
      );
    default:
      return <p role="status">Этот формат задания пока недоступен для прохождения.</p>;
  }
}

function LessonAttempt({ module, exercises, onBack }) {
  const [step, setStep] = useState(0);
  const [answers, setAnswers] = useState([]);
  const questionId = useId();
  const currentHeading = useRef(null);
  const total = exercises.length;
  const exercise = exercises[step];
  const answered = exercises.filter((item, index) => hasAnswer(item, answers[index])).length;

  useEffect(() => {
    currentHeading.current?.focus();
  }, [step]);

  function changeAnswer(value) {
    setAnswers(previous => {
      const updated = [...previous];
      updated[step] = value;
      return updated;
    });
  }

  return (
    <section className="lesson">
      <header className="lesson-heading">
        <p className="lesson-meta">Урок{module.difficulty && ` · ${module.difficulty}`}</p>
        <h1 className="lesson-title">{module.title}</h1>
        <button className="lesson-secondary" type="button" onClick={onBack}>К материалу</button>
      </header>
      {exercise ? (
        <div className="lesson-layout">
          <article className="lesson-card" aria-labelledby={questionId}>
            <p className="lesson-category">{exerciseNames.get(exercise.type) ?? 'Упражнение'} · {step + 1} из {total}</p>
            <h2 ref={currentHeading} tabIndex={-1} id={questionId} className="lesson-question">{exercise.question_text}</h2>
            <p className="lesson-instruction">{exercise.instruction}</p>
            <AnswerForm exercise={exercise} answer={answers[step]} onChange={changeAnswer} questionId={questionId} />
            <div className="lesson-actions">
              <button type="button" className="lesson-secondary" disabled={step === 0}
                onClick={() => setStep(step - 1)}>Назад</button>
              <button type="button" className="lesson-primary"
                onClick={() => setStep(step + 1)}>{step + 1 === total ? 'Завершить' : 'Далее'}</button>
            </div>
          </article>
          <aside className="lesson-progress">
            <h2>Ваш прогресс</h2>
            <p>Заполнено заданий</p>
            <strong className="lesson-progress-count">{answered} / {total}</strong>
            <progress className="lesson-meter" aria-label="Заполнено заданий" max={total} value={answered} />
            <p>Ответы сохраняются в этой вкладке до нового урока или перезагрузки. Автоматическая проверка пока не подключена.</p>
          </aside>
        </div>
      ) : (
        <div className="lesson-card lesson-complete">
          <h2 ref={currentHeading} tabIndex={-1}>{total > 0 ? 'Просмотр завершён' : 'В модуле пока нет упражнений'}</h2>
          {total > 0 && (
            <>
              <p>Заполнено заданий: {answered} из {total}.</p>
              <progress className="lesson-meter" aria-label="Заполнено заданий" max={total} value={answered} />
              <div className="lesson-actions">
                <button className="lesson-secondary" type="button" onClick={() => {
                  const firstEmpty = exercises.findIndex((item, index) => !hasAnswer(item, answers[index]));
                  setStep(firstEmpty === -1 ? 0 : firstEmpty);
                }}>{answered === total ? 'Посмотреть ответы' : 'К пропущенному заданию'}</button>
                <button className="lesson-primary" type="button" onClick={onBack}>К материалу</button>
              </div>
            </>
          )}
        </div>
      )}
    </section>
  );
}

export default function Lesson({ module, exercises, onBack }) {
  return <LessonAttempt key={module.id} module={module} exercises={exercises} onBack={onBack} />;
}
