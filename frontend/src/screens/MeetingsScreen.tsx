import { useState } from 'react';
import { MeetingsService } from '../../bindings/daily-agent/internal/app';
import { PlusIcon } from '../components/Icons';
import { MeetingRow } from '../components/MeetingRow';
import { Segmented } from '../components/Segmented';
import { useCards } from '../lib/cards';
import { useLoad } from '../lib/data';
import { countdownLabel, dayHeading, toDay, upcomingLabel } from '../lib/format';
import { currentMeeting, useNow } from './TodayScreen';

type Period = 'upcoming' | 'past';

/** Экран «Встречи»: список по дням, предстоящие или прошедшие. */
export function MeetingsScreen() {
  const [period, setPeriod] = useState<Period>('upcoming');
  const now = useNow();
  const today = toDay(now);
  const { openMeeting } = useCards();
  const { data, error } = useLoad(() => MeetingsService.List(period), [period]);

  const days = data?.days ?? [];
  const all = days.flatMap((d) => d.meetings ?? []);
  const current = period === 'upcoming' ? currentMeeting(all, now) : undefined;

  return (
    <>
      <div className="screen-header drag-region">
        <div className="screen-header__title">
          <h2 className="screen-title">Встречи</h2>
          {data && <span className="screen-subtitle">{upcomingLabel(data.upcomingCount)}</span>}
        </div>
        <button className="outline-button" onClick={() => openMeeting()}>
          <PlusIcon />
          <span>Встреча</span>
        </button>
      </div>

      <Segmented
        label="Период"
        value={period}
        onChange={setPeriod}
        options={[{ value: 'upcoming', label: 'Предстоящие' }, { value: 'past', label: 'Прошедшие' }]}
      />

      {error && <p className="screen-error" role="alert">Не удалось загрузить встречи: {error}</p>}
      {data && days.length === 0 && (
        <p className="placeholder">{period === 'upcoming' ? 'Предстоящих встреч нет.' : 'Прошедших встреч за последний месяц нет.'}</p>
      )}

      {days.map((d) => (
        <section key={d.day} className="section meetings-day">
          <h3 className="section__title meetings-day__title">{dayHeading(d.day, today)}</h3>
          {(d.meetings ?? []).map((m) => (
            <MeetingRow
              key={`${m.id}-${m.startsAt}`}
              meeting={m}
              variant="list"
              current={m === current}
              countdown={m === current ? countdownLabel(m.startsAt, m.durationMin, now) : null}
            />
          ))}
        </section>
      ))}
    </>
  );
}
