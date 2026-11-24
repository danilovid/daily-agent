import { useEffect, useState } from 'react';
import { LinksService, TagsService, TasksService } from '../../bindings/daily-agent/internal/app';
import type { TagView, TaskDetail } from '../../bindings/daily-agent/internal/app';
import { errorText, useData } from '../lib/data';
import { createdLabel, dueDateLabel, toDay } from '../lib/format';
import { SOURCE_LABELS } from '../lib/labels';
import { DateField, Field } from './Fields';
import { CloseIcon } from './Icons';
import { LinksEditor, type EditableLink } from './LinksEditor';
import { Modal } from './Modal';
import { TagPicker } from './TagEditor';

type Props = { taskId?: string; onClose: () => void };

type Form = {
  title: string;
  note: string;
  dueDate: string;
  tagIds: string[];
  links: EditableLink[];
  done: boolean;
};

const EMPTY: Form = { title: '', note: '', dueDate: '', tagIds: [], links: [], done: false };

function fromDetail(d: TaskDetail): Form {
  return {
    title: d.title,
    note: d.note,
    dueDate: d.dueDate,
    tagIds: (d.tags ?? []).map((t) => t.id),
    links: (d.links ?? []).map((l) => ({ id: l.id, title: l.title, url: l.url })),
    done: d.status === 'done',
  };
}

/** Откуда задача: «Своя задача», «Трекер, T-101, вчера, 18:40». */
function sourceText(d: TaskDetail, today: string): string {
  const created = createdLabel(d.createdAt, today);
  if (d.source === 'own') return `Своя задача, ${created}`;
  const kind = d.kind === 'mr' ? 'мерж-реквест ' : '';
  return `${SOURCE_LABELS[d.source] ?? d.source}: ${kind}${d.externalId}, ${created}`;
}

/** Карточка задачи: новая (без taskId) или существующая. */
export function TaskCard({ taskId, onClose }: Props) {
  const { refresh, showError } = useData();
  const [detail, setDetail] = useState<TaskDetail | null>(null);
  const [form, setForm] = useState<Form | null>(taskId ? null : EMPTY);
  const [initial, setInitial] = useState(taskId ? '' : JSON.stringify(EMPTY));
  const [tags, setTags] = useState<TagView[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const today = toDay(new Date());

  useEffect(() => {
    TagsService.List().then((t) => setTags(t ?? []));
    if (!taskId) return;
    TasksService.Get(taskId)
      .then((d) => {
        const f = fromDetail(d);
        setDetail(d);
        setForm(f);
        setInitial(JSON.stringify(f));
      })
      .catch((e) => {
        showError(e);
        onClose();
      });
  }, [taskId, onClose, showError]);

  const dirty = form !== null && JSON.stringify(form) !== initial;
  const set = <K extends keyof Form>(key: K, value: Form[K]) => setForm((f) => (f ? { ...f, [key]: value } : f));
  const titleEditable = !detail || detail.titleEditable;
  // Отметка о выполнении есть у задач из бэклога и выполненных.
  const canComplete = detail && (detail.status === 'backlog' || detail.status === 'done');

  const save = async () => {
    if (!form) return;
    setBusy(true);
    setError(null);
    const input = {
      title: form.title,
      note: form.note,
      dueDate: form.dueDate,
      tagIds: form.tagIds,
      links: form.links.map((l) => ({ id: l.id, title: l.title, url: l.url })),
    };
    try {
      if (taskId && detail) {
        await TasksService.Update(taskId, input);
        const wasDone = detail.status === 'done';
        if (canComplete && form.done !== wasDone) {
          await TasksService.SetStatus(taskId, form.done ? 'done' : 'backlog');
        }
      } else {
        await TasksService.Create(input);
      }
      refresh();
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!taskId) return;
    if (!confirmDelete) {
      setConfirmDelete(true);
      return;
    }
    setBusy(true);
    try {
      await TasksService.Delete(taskId);
      refresh();
      onClose();
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  return (
    <Modal label="Карточка задачи" onClose={onClose} dismissOnOverlay={!dirty}>
      {form && (
        <form
          className="card"
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          <div className="card__header">
            {canComplete && (
              <input
                type="checkbox"
                className="checkbox checkbox--lg"
                aria-label="Отметить выполненной"
                checked={form.done}
                onChange={(e) => set('done', e.target.checked)}
              />
            )}
            <label htmlFor="task-title" className="visually-hidden">Название задачи</label>
            <input
              id="task-title"
              className="card__title"
              type="text"
              placeholder="Название задачи"
              autoFocus={!taskId}
              readOnly={!titleEditable}
              title={titleEditable ? undefined : 'Название приходит из источника'}
              value={form.title}
              onChange={(e) => set('title', e.target.value)}
            />
            <button type="button" className="icon-button card__close" aria-label="Закрыть карточку" onClick={onClose}>
              <CloseIcon />
            </button>
          </div>

          <div className="field field--tight">
            <h3 className="field__label">Теги</h3>
            <TagPicker
              tags={tags}
              selected={form.tagIds}
              onChange={(ids) => set('tagIds', ids)}
              onTagCreated={(tag) => {
                setTags((t) => [...t, tag]);
                refresh();
              }}
            />
          </div>

          <Field label="Ссылки">
            <LinksEditor
              links={form.links}
              onChange={(links) => set('links', links)}
              onOpen={(l) => LinksService.OpenTaskLink(l.id).catch(showError)}
              source={detail?.url ? {
                label: SOURCE_LABELS[detail.source] ?? 'Источник',
                url: detail.url,
                onOpen: () => LinksService.OpenTaskSource(detail.id).catch(showError),
              } : undefined}
              defaultTitle="Ссылка"
            />
          </Field>

          <div className="card__grid card__grid--2">
            <Field label="Срок">
              <DateField
                value={form.dueDate}
                display={dueDateLabel(form.dueDate, today)}
                ariaLabel="Срок"
                onChange={(v) => set('dueDate', v)}
                onClear={() => set('dueDate', '')}
              />
            </Field>
            <Field label="Источник">
              <p className="card__source">{detail ? sourceText(detail, today) : 'Своя задача'}</p>
            </Field>
          </div>

          <Field label="Заметка" htmlFor="task-note">
            <textarea
              id="task-note"
              className="text-input textarea"
              placeholder="Что важно помнить"
              value={form.note}
              onChange={(e) => set('note', e.target.value)}
            />
          </Field>

          {error && <p className="field__error" role="alert">{error}</p>}

          <div className="card__footer">
            {taskId ? (
              <button type="button" className="danger-button" onClick={remove} disabled={busy}>
                {confirmDelete ? 'Точно удалить?' : 'Удалить задачу'}
              </button>
            ) : <span />}
            <button type="submit" className="primary-button" disabled={busy}>Готово</button>
          </div>
        </form>
      )}
    </Modal>
  );
}
