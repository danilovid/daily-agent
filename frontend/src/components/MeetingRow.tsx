import { LinksService } from '../../bindings/daily-agent/internal/app';
import type { MeetingView } from '../../bindings/daily-agent/internal/app';
import { useCards } from '../lib/cards';
import { useData } from '../lib/data';
import { durationLabel, timeOfDay } from '../lib/format';
import { PlusIcon, VideoIcon } from './Icons';

type Props = {
  meeting: MeetingView;
  /** agenda — строка на экране «Сегодня», list — на экране «Встречи». */
  variant: 'agenda' | 'list';
  /** Ближайшая встреча: выделена фоном, кнопка «Подключиться» — основная. */
  current?: boolean;
  /** Подпись «через 42 мин» или «идёт». */
  countdown?: string | null;
  /** Подпись под временем вместо длительности, например дата в результатах поиска. */
  caption?: string;
};

export function MeetingRow({ meeting: m, variant, current, countdown, caption }: Props) {
  const { openMeeting } = useCards();
  const { showError } = useData();
  const link = m.links?.[0];

  return (
    <div className={`meeting-row meeting-row--${variant}${current ? ' meeting-row--current' : ''}`}>
      <div className="meeting-row__time">
        <span className="meeting-row__start">{timeOfDay(m.startsAt)}</span>
        <span className="meeting-row__duration">{caption ?? durationLabel(m.durationMin)}</span>
      </div>
      <button className="meeting-row__title" onClick={() => openMeeting(m.id)}>{m.title}</button>
      {countdown && <span className="soft-badge">{countdown}</span>}
      {link ? (
        <button
          className="join-button"
          aria-label={`Подключиться к созвону: ${m.title}`}
          onClick={() => LinksService.OpenMeetingLink(link.id).catch(showError)}
        >
          <span className={`join-button__body${current || variant === 'agenda' ? '' : ' join-button__body--secondary'}`}>
            <VideoIcon />Подключиться
          </span>
        </button>
      ) : variant === 'list' && (
        <button className="text-button text-button--accent" onClick={() => openMeeting(m.id)}>
          <PlusIcon size="var(--icon-sm)" />
          <span>Ссылка на созвон</span>
        </button>
      )}
    </div>
  );
}
