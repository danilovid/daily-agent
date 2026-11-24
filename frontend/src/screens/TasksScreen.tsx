import { useState } from 'react';
import { TasksService } from '../../bindings/daily-agent/internal/app';
import type { TagView } from '../../bindings/daily-agent/internal/app';
import { CloseIcon, PlusIcon } from '../components/Icons';
import { Segmented } from '../components/Segmented';
import { TagChip } from '../components/TagChip';
import { InboxRow, TaskRow } from '../components/TaskRows';
import { useCards } from '../lib/cards';
import { useLoad } from '../lib/data';
import { toDay } from '../lib/format';

type Props = {
  mode: 'inbox' | 'backlog';
  /** Тег, по которому отфильтрован бэклог. */
  tag?: TagView | null;
  onClearTag?: () => void;
};

/** Экраны «Входящие» и «Бэклог». */
export function TasksScreen({ mode, tag, onClearTag }: Props) {
  const [view, setView] = useState<'backlog' | 'done'>('backlog');
  const status = mode === 'inbox' ? 'inbox' : view;
  const tagID = mode === 'backlog' ? tag?.id ?? '' : '';
  const { openTask } = useCards();
  const { data, error } = useLoad(() => TasksService.List(status, tagID), [status, tagID]);
  const tasks = data ?? [];
  const today = toDay(new Date());

  const empty =
    mode === 'inbox' ? 'Во «Входящих» пусто: новые задачи из трекера и репозитория появятся здесь.'
      : view === 'done' ? 'Выполненных задач нет.'
        : tag ? `В бэклоге нет задач с тегом «${tag.name}».` : 'В бэклоге пусто.';

  return (
    <>
      <div className="screen-header drag-region">
        <div className="screen-header__title">
          <h2 className="screen-title">{mode === 'inbox' ? 'Входящие' : 'Бэклог'}</h2>
          {data && <span className="screen-subtitle">{tasks.length}</span>}
        </div>
        {mode === 'backlog' && (
          <button className="outline-button" onClick={() => openTask()}>
            <PlusIcon />
            <span>Задача</span>
          </button>
        )}
      </div>

      {mode === 'backlog' && (
        <div className="screen-toolbar">
          <Segmented
            label="Задачи"
            value={view}
            onChange={setView}
            options={[{ value: 'backlog', label: 'Открытые' }, { value: 'done', label: 'Выполненные' }]}
          />
          {tag && (
            <button className="filter-chip" onClick={onClearTag} aria-label={`Убрать фильтр по тегу ${tag.name}`}>
              <TagChip tag={tag} variant="sidebar" />
              <CloseIcon size="var(--icon-chevron)" />
            </button>
          )}
        </div>
      )}

      {error && <p className="screen-error" role="alert">Не удалось загрузить задачи: {error}</p>}

      <section className="section">
        {data && tasks.length === 0 && <p className="section__empty section__empty--ruled">{empty}</p>}
        {tasks.map((t) => (mode === 'inbox'
          ? <InboxRow key={t.id} task={t} today={today} />
          : <TaskRow key={t.id} task={t} today={today} />))}
      </section>
    </>
  );
}
