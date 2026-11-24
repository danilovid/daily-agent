import { useEffect, useState } from 'react';
import { ChatsService, DayPlanService, TasksService, TodayService } from '../../bindings/daily-agent/internal/app';
import type { MeetingView, PlanView, TaskView } from '../../bindings/daily-agent/internal/app';
import { CloseIcon, PlusIcon } from '../components/Icons';
import { MeetingRow } from '../components/MeetingRow';
import { InboxRow, TaskRow } from '../components/TaskRows';
import { agentUnavailable, useAgentStatus } from '../lib/agent';
import { useCards } from '../lib/cards';
import { useAction, useData, useLoad } from '../lib/data';
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

      {data && <DayPlan plan={data.plan} />}
      {data && <Agenda meetings={data.meetings ?? []} now={now} />}
      {data && <Inbox tasks={data.inbox ?? []} today={data.day} />}
      {data && (
        <Backlog
          tasks={data.backlog ?? []}
          today={data.day}
          planned={new Set((data.plan?.items ?? []).map((it) => it.task.id))}
        />
      )}
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

function Backlog({ tasks, today, planned }: { tasks: TaskView[]; today: string; planned: Set<string> }) {
  const { openTask } = useCards();
  const act = useAction();
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
      {tasks.map((t) => (
        <TaskRow
          key={t.id}
          task={t}
          today={today}
          onPlan={planned.has(t.id) ? undefined : () => act(() => DayPlanService.Add(t.id))}
        />
      ))}
    </section>
  );
}

/** Длительность: «45 мин», «2 ч», «1 ч 30 мин». */
export function minutesLabel(min: number): string {
  const h = Math.floor(min / 60);
  const m = min % 60;
  if (h === 0) return `${m} мин`;
  return m === 0 ? `${h} ч` : `${h} ч ${m} мин`;
}

/** План на день: задачи, которые выбрал агент или автор, с оценкой и причиной. */
function DayPlan({ plan }: { plan: PlanView | null }) {
  const { openChat } = useCards();
  const { showError } = useData();
  const act = useAction();
  const status = useAgentStatus();
  const blocked = agentUnavailable(status);
  const [asking, setAsking] = useState(false);
  const items = plan?.items ?? [];

  const ask = () => {
    setAsking(true);
    ChatsService.PlanDay()
      .then((chat) => openChat(chat.id))
      .catch(showError)
      .finally(() => setAsking(false));
  };

  return (
    <section className="section">
      <div className="section__header section__header--lg">
        <h3 className="section__title">План на день</h3>
        {items.length > 0 && <span className="section__count">{items.length}</span>}
        {plan && items.length > 0 && (
          <span className="section__hint">
            {minutesLabel(plan.totalMin)} из {minutesLabel(plan.freeMin)} свободных
          </span>
        )}
        <button
          type="button"
          className="text-button text-button--accent plan__ask"
          onClick={ask}
          disabled={asking || !!blocked}
          title={blocked ?? undefined}
        >
          {asking ? 'Прошу агента…' : plan ? 'Составить заново' : 'Составить план'}
        </button>
      </div>
      {items.length === 0 && (
        <p className="section__empty section__empty--ruled">
          {plan ? 'В плане нет задач.' : 'Плана пока нет. Агент выберет из бэклога задачи, которые помещаются в свободное время.'}
        </p>
      )}
      {items.map((it) => (
        <div key={it.task.id} className={`plan-row${it.task.status === 'done' ? ' plan-row--done' : ''}`}>
          <input
            type="checkbox"
            className="checkbox"
            checked={it.task.status === 'done'}
            aria-label={`${it.task.status === 'done' ? 'Вернуть в работу' : 'Отметить выполненной'}: ${it.task.title}`}
            onChange={(e) => act(() => TasksService.SetStatus(it.task.id, e.target.checked ? 'done' : 'backlog'))}
          />
          <div className="plan-row__text">
            <span className="plan-row__title">{it.task.title}</span>
            {it.reason && <span className="plan-row__reason">{it.reason}</span>}
          </div>
          {it.estimateMin > 0 && <span className="soft-badge">{minutesLabel(it.estimateMin)}</span>}
          <button type="button" className="icon-button" aria-label={`Убрать из плана: ${it.task.title}`} onClick={() => act(() => DayPlanService.Remove(it.task.id))}>
            <CloseIcon size="var(--icon-sm)" />
          </button>
        </div>
      ))}
    </section>
  );
}
