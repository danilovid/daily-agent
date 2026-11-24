import { LinksService, TasksService } from '../../bindings/daily-agent/internal/app';
import type { TaskView } from '../../bindings/daily-agent/internal/app';
import { useCards } from '../lib/cards';
import { useAction, useData } from '../lib/data';
import { createdLabel, dueLabel } from '../lib/format';
import { SOURCE_LABELS, STATUS_LABELS } from '../lib/labels';
import { CheckIcon, CloseIcon, ExternalIcon } from './Icons';
import { TagChip } from './TagChip';

/** Кнопка ссылки в строке задачи: источник внешней записи или первая своя ссылка. */
function TaskLinkChip({ task }: { task: TaskView }) {
  const { showError } = useData();
  const first = task.links?.[0];
  if (task.url) {
    const label = SOURCE_LABELS[task.source] ?? 'Источник';
    return (
      <button className="link-chip" aria-label={`Открыть задачу в источнике: ${label}`}
        onClick={() => LinksService.OpenTaskSource(task.id).catch(showError)}>
        <span className="link-chip__body"><ExternalIcon />{label}</span>
      </button>
    );
  }
  if (first) {
    return (
      <button className="link-chip" aria-label={`Открыть ссылку: ${first.title}`}
        onClick={() => LinksService.OpenTaskLink(first.id).catch(showError)}>
        <span className="link-chip__body"><ExternalIcon />{first.title}</span>
      </button>
    );
  }
  return null;
}

/** Строка входящей задачи: принять в бэклог или отклонить. */
export function InboxRow({ task: t, today }: { task: TaskView; today: string }) {
  const { openTask } = useCards();
  const act = useAction();
  return (
    <div className="inbox-row">
      <div className="inbox-row__body">
        <button className="inbox-row__title" onClick={() => openTask(t.id)}>{t.title}</button>
        <span className="inbox-row__meta">
          {SOURCE_LABELS[t.source] ?? t.source} · {createdLabel(t.createdAt, today)}
          {t.tags?.map((tag) => <TagChip key={tag.id} tag={tag} variant="meta" />)}
        </span>
      </div>
      <button className="icon-button icon-button--accent" aria-label={`Принять в бэклог: ${t.title}`}
        onClick={() => act(() => TasksService.SetStatus(t.id, 'backlog'))}>
        <CheckIcon />
      </button>
      <button className="icon-button icon-button--edge" aria-label={`Отклонить: ${t.title}`}
        onClick={() => act(() => TasksService.SetStatus(t.id, 'rejected'))}>
        <CloseIcon />
      </button>
    </div>
  );
}

/** Строка задачи из бэклога или выполненной: отметка, название, ссылка, теги, срок. */
export function TaskRow({ task: t, today, showStatus }: { task: TaskView; today: string; showStatus?: boolean }) {
  const { openTask } = useCards();
  const act = useAction();
  const due = t.status === 'done' ? null : dueLabel(t.dueDate, today);
  const canToggle = t.status === 'backlog' || t.status === 'done';
  return (
    <div className={`task-row${t.status === 'done' ? ' task-row--done' : ''}`}>
      <input
        type="checkbox"
        className="checkbox"
        checked={t.status === 'done'}
        disabled={!canToggle}
        aria-label={`${t.status === 'done' ? 'Вернуть в работу' : 'Отметить выполненной'}: ${t.title}`}
        onChange={(e) => act(() => TasksService.SetStatus(t.id, e.target.checked ? 'done' : 'backlog'))}
      />
      <button className="task-row__title" onClick={() => openTask(t.id)}>{t.title}</button>
      {showStatus && <span className="task-row__status">{STATUS_LABELS[t.status] ?? t.status}</span>}
      <TaskLinkChip task={t} />
      {t.tags?.map((tag) => <TagChip key={tag.id} tag={tag} variant="row" />)}
      {due && <span className="soft-badge">{due.text}</span>}
    </div>
  );
}
