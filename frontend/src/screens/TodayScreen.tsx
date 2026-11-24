import { useEffect, useState } from 'react';
import type { MeetingView, TaskView, TodayView } from '../../bindings/daily-agent/internal/app/models';
import { TagChip } from '../components/TagChip';
import { CheckIcon, CloseIcon, ExternalIcon, PlusIcon, VideoIcon } from '../components/Icons';
import { createdLabel, dueLabel, longDate, meetingCountdown, timeOfDay } from '../lib/format';

type Props = {
  data: TodayView | null;
  error: string | null;
};

// Подписи источников внешних записей.
const SOURCE_LABELS: Record<string, string> = {
  tracker: 'Трекер',
  repo: 'GitLab',
  own: 'Своя',
};

/** Текущее время, которое обновляется раз в 30 секунд. */
function useNow(): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = window.setInterval(() => setNow(new Date()), 30_000);
    return () => window.clearInterval(id);
  }, []);
  return now;
}

export function TodayScreen({ data, error }: Props) {
  const now = useNow();

  return (
    <>
      <div className="screen-header drag-region">
        <div className="screen-header__title">
          <h2 className="screen-title">Сегодня</h2>
          {data && <span className="screen-subtitle">{longDate(data.day)}</span>}
        </div>
        <button className="outline-button">
          <PlusIcon />
          <span>Встреча</span>
        </button>
      </div>

      {error && <p className="screen-error" role="alert">Не удалось загрузить данные: {error}</p>}

      {data?.summary && (
        <section className="summary-card">
          <h3 className="summary-card__title">Сводка от агента</h3>
          <p className="summary-card__text">{data.summary.text}</p>
        </section>
      )}

      {data && <Meetings meetings={data.meetings ?? []} now={now} />}
      {data && <Inbox tasks={data.inbox ?? []} today={data.day} />}
      {data && <Backlog tasks={data.backlog ?? []} today={data.day} />}
    </>
  );
}

function Meetings({ meetings, now }: { meetings: MeetingView[]; now: Date }) {
  // Выделяем ближайшую встречу: идущую сейчас или следующую.
  const current = meetings.find((m) => meetingCountdown(m.startsAt, m.durationMin, now) !== null);

  return (
    <section className="section">
      <h3 className="section__title section__title--spaced">Встречи</h3>
      {meetings.length === 0 && <p className="section__empty">Встреч нет</p>}
      {meetings.map((m) => {
        const isCurrent = m === current;
        const countdown = isCurrent ? meetingCountdown(m.startsAt, m.durationMin, now) : null;
        const link = m.links?.[0];
        return (
          <div key={m.id} className={`meeting-row${isCurrent ? ' meeting-row--current' : ''}`}>
            <div className="meeting-row__time">
              <span className="meeting-row__start">{timeOfDay(m.startsAt)}</span>
              <span className="meeting-row__duration">{m.durationMin} мин</span>
            </div>
            <span className="meeting-row__title">{m.title}</span>
            {countdown && <span className="soft-badge">{countdown}</span>}
            {link && (
              <button className="join-button" aria-label={`Подключиться к созвону: ${m.title}`}>
                <span className="join-button__body"><VideoIcon />Подключиться</span>
              </button>
            )}
          </div>
        );
      })}
    </section>
  );
}

function Inbox({ tasks, today }: { tasks: TaskView[]; today: string }) {
  return (
    <section className="section">
      <div className="section__header">
        <h3 className="section__title">Входящие</h3>
        {tasks.length > 0 && <span className="soft-count">{tasks.length}</span>}
        <span className="section__hint">ждут разбора</span>
      </div>
      {tasks.length === 0 && <p className="section__empty section__empty--ruled">Во «Входящих» пусто</p>}
      {tasks.map((t) => (
        <div key={t.id} className="inbox-row">
          <div className="inbox-row__body">
            <span className="inbox-row__title">{t.title}</span>
            <span className="inbox-row__meta">
              {SOURCE_LABELS[t.source] ?? t.source} · {createdLabel(t.createdAt, today)}
              {t.tags?.map((tag) => <TagChip key={tag.id} tag={tag} variant="meta" />)}
            </span>
          </div>
          <button className="icon-button icon-button--accent" aria-label="Принять в бэклог">
            <CheckIcon />
          </button>
          <button className="icon-button icon-button--edge" aria-label="Отклонить">
            <CloseIcon />
          </button>
        </div>
      ))}
    </section>
  );
}

function Backlog({ tasks, today }: { tasks: TaskView[]; today: string }) {
  return (
    <section className="section">
      <div className="section__header section__header--lg">
        <h3 className="section__title">Бэклог</h3>
        <span className="section__count">{tasks.length}</span>
        <button className="icon-button icon-button--edge section__add" aria-label="Добавить задачу">
          <PlusIcon size="var(--icon-nav)" />
        </button>
      </div>
      {tasks.length === 0 && <p className="section__empty section__empty--ruled">В бэклоге пусто</p>}
      {tasks.map((t) => {
        const due = dueLabel(t.dueDate, today);
        const source = SOURCE_LABELS[t.source] ?? t.source;
        return (
          <div key={t.id} className="task-row">
            <label className="task-row__label">
              <input
                type="checkbox"
                className="checkbox"
                checked={t.status === 'done'}
                onChange={() => { /* Отметка выполнения появится на шаге 2. */ }}
                aria-label={`Отметить выполненной: ${t.title}`}
              />
              <span className="task-row__title">{t.title}</span>
            </label>
            {t.url && (
              <button className="link-chip" aria-label={`Открыть задачу в источнике: ${source}`}>
                <span className="link-chip__body"><ExternalIcon />{source}</span>
              </button>
            )}
            {t.tags?.map((tag) => <TagChip key={tag.id} tag={tag} variant="row" />)}
            {due && <span className="soft-badge">{due.text}</span>}
          </div>
        );
      })}
    </section>
  );
}
