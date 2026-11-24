import { useEffect, useState } from 'react';
import { TodayService } from '../../bindings/daily-agent/internal/app';
import type { MeetingView, TaskView } from '../../bindings/daily-agent/internal/app';
import { PlusIcon } from '../components/Icons';
import { MeetingRow } from '../components/MeetingRow';
import { InboxRow, TaskRow } from '../components/TaskRows';
import { useCards } from '../lib/cards';
import { useLoad } from '../lib/data';
import { countdownLabel, longDate, meetingCountdown, toDay } from '../lib/format';

/** Текущее время, которое обновляется раз в 30 секунд. */
export function useNow(): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = window.setInterval(() => setNow(new Date()), 30_000);
    return () => window.clearInterval(id);
  }, []);
  return now;
}

/** Ближайшая сегодняшняя встреча: идёт сейчас или начнётся первой. */
export function currentMeeting(meetings: MeetingView[], now: Date): MeetingView | undefined {
  const today = toDay(now);
  return meetings.find((m) => toDay(new Date(m.startsAt)) === today && meetingCountdown(m.startsAt, m.durationMin, now) !== null);
}

export function TodayScreen() {
  const now = useNow();
  const { openMeeting } = useCards();
  const { data, error } = useLoad(() => TodayService.Today(''), []);

  return (
    <>
      <div className="screen-header drag-region">
        <div className="screen-header__title">
          <h2 className="screen-title">Сегодня</h2>
          {data && <span className="screen-subtitle">{longDate(data.day)}</span>}
        </div>
        <button className="outline-button" onClick={() => openMeeting()}>
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

      {data && <Agenda meetings={data.meetings ?? []} now={now} />}
      {data && <Inbox tasks={data.inbox ?? []} today={data.day} />}
      {data && <Backlog tasks={data.backlog ?? []} today={data.day} />}
    </>
  );
}

function Agenda({ meetings, now }: { meetings: MeetingView[]; now: Date }) {
  const current = currentMeeting(meetings, now);
  return (
    <section className="section">
      <h3 className="section__title section__title--spaced">Встречи</h3>
      {meetings.length === 0 && <p className="section__empty">Встреч нет</p>}
      {meetings.map((m) => (
        <MeetingRow
          key={`${m.id}-${m.startsAt}`}
          meeting={m}
          variant="agenda"
          current={m === current}
          countdown={m === current ? countdownLabel(m.startsAt, m.durationMin, now) : null}
        />
      ))}
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
      {tasks.map((t) => <InboxRow key={t.id} task={t} today={today} />)}
    </section>
  );
}

function Backlog({ tasks, today }: { tasks: TaskView[]; today: string }) {
  const { openTask } = useCards();
  return (
    <section className="section">
      <div className="section__header section__header--lg">
        <h3 className="section__title">Бэклог</h3>
        <span className="section__count">{tasks.length}</span>
        <button className="icon-button icon-button--edge section__add" aria-label="Добавить задачу" onClick={() => openTask()}>
          <PlusIcon size="var(--icon-nav)" />
        </button>
      </div>
      {tasks.length === 0 && <p className="section__empty section__empty--ruled">В бэклоге пусто</p>}
      {tasks.map((t) => <TaskRow key={t.id} task={t} today={today} />)}
    </section>
  );
}
