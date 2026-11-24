import { SearchService } from '../../bindings/daily-agent/internal/app';
import { MeetingRow } from '../components/MeetingRow';
import { TaskRow } from '../components/TaskRows';
import { useLoad } from '../lib/data';
import { shortDate, toDay } from '../lib/format';

/** Результаты поиска из боковой панели. */
export function SearchScreen({ query }: { query: string }) {
  const { data, error } = useLoad(() => SearchService.Search(query), [query]);
  const tasks = data?.tasks ?? [];
  const meetings = data?.meetings ?? [];
  const today = toDay(new Date());

  return (
    <>
      <div className="screen-header drag-region">
        <div className="screen-header__title">
          <h2 className="screen-title">Поиск</h2>
          <span className="screen-subtitle">«{query}»</span>
        </div>
      </div>

      {error && <p className="screen-error" role="alert">Не удалось выполнить поиск: {error}</p>}
      {data && tasks.length === 0 && meetings.length === 0 && (
        <p className="placeholder">Ничего не нашлось. Поиск идёт по началу слов в названиях и заметках задач и встреч.</p>
      )}

      {tasks.length > 0 && (
        <section className="section">
          <div className="section__header">
            <h3 className="section__title">Задачи</h3>
            <span className="section__count">{tasks.length}</span>
          </div>
          {tasks.map((t) => <TaskRow key={t.id} task={t} today={today} showStatus />)}
        </section>
      )}

      {meetings.length > 0 && (
        <section className="section meetings-day">
          <div className="section__header">
            <h3 className="section__title">Встречи</h3>
            <span className="section__count">{meetings.length}</span>
          </div>
          {meetings.map((m) => (
            <MeetingRow key={m.id} meeting={m} variant="list" caption={shortDate(toDay(new Date(m.startsAt)))} />
          ))}
        </section>
      )}
    </>
  );
}
