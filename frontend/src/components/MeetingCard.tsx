import { useEffect, useMemo, useState } from 'react';
import { LinksService, MeetingsService } from '../../bindings/daily-agent/internal/app';
import type { MeetingDetail, RepeatOption } from '../../bindings/daily-agent/internal/app';
import { errorText, useData } from '../lib/data';
import { durationLabel, hhmm, nextHalfHour, pickerDate, toDay } from '../lib/format';
import { DateField, Field, SelectField } from './Fields';
import { CloseIcon } from './Icons';
import { LinksEditor, type EditableLink } from './LinksEditor';
import { Modal } from './Modal';

type Props = { meetingId?: string; onClose: () => void };

type Form = {
  title: string;
  date: string;
  start: string;
  durationMin: number;
  repeat: string;
  note: string;
  links: EditableLink[];
};

const DURATIONS = [15, 30, 45, 60, 90, 120, 180, 240];

function timeOptions(current: string): { value: string; label: string }[] {
  const values = new Set<string>();
  for (let m = 0; m < 24 * 60; m += 15) {
    values.add(`${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`);
  }
  if (current) values.add(current);
  return [...values].sort().map((v) => ({ value: v, label: v }));
}

function newForm(): Form {
  const start = nextHalfHour(new Date());
  return { title: '', date: toDay(start), start: hhmm(start), durationMin: 30, repeat: '', note: '', links: [] };
}

function fromDetail(d: MeetingDetail): Form {
  return {
    title: d.title,
    date: d.date,
    start: d.start,
    durationMin: d.durationMin,
    repeat: d.repeat,
    note: d.note,
    links: (d.links ?? []).map((l) => ({ id: l.id, title: l.title, url: l.url })),
  };
}

/** Карточка встречи: новая (без meetingId) или существующая. */
export function MeetingCard({ meetingId, onClose }: Props) {
  const { refresh, showError } = useData();
  const [form, setForm] = useState<Form | null>(meetingId ? null : newForm());
  const [initial, setInitial] = useState<string>(meetingId ? '' : JSON.stringify(newForm()));
  const [editable, setEditable] = useState(true);
  const [repeatOptions, setRepeatOptions] = useState<RepeatOption[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    MeetingsService.RepeatOptions().then((o) => setRepeatOptions(o ?? []));
    if (!meetingId) return;
    MeetingsService.Get(meetingId)
      .then((d) => {
        const f = fromDetail(d);
        setForm(f);
        setInitial(JSON.stringify(f));
        setEditable(d.editable);
      })
      .catch((e) => {
        showError(e);
        onClose();
      });
  }, [meetingId, onClose, showError]);

  const dirty = form !== null && JSON.stringify(form) !== initial;
  const set = <K extends keyof Form>(key: K, value: Form[K]) => setForm((f) => (f ? { ...f, [key]: value } : f));

  const durations = useMemo(() => {
    const list = new Set(DURATIONS);
    if (form) list.add(form.durationMin);
    return [...list].sort((a, b) => a - b).map((m) => ({ value: m, label: durationLabel(m) }));
  }, [form]);

  const repeats = useMemo(() => {
    const list = [...repeatOptions];
    if (form && !list.some((o) => o.value === form.repeat)) list.push({ value: form.repeat, label: 'Свой повтор' });
    return list;
  }, [repeatOptions, form]);

  const save = async () => {
    if (!form) return;
    setBusy(true);
    setError(null);
    const input = { ...form, links: form.links.map((l) => ({ id: l.id, title: l.title, url: l.url })) };
    try {
      if (meetingId) await MeetingsService.Update(meetingId, input);
      else await MeetingsService.Create(input);
      refresh();
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!meetingId) return;
    if (!confirmDelete) {
      setConfirmDelete(true);
      return;
    }
    setBusy(true);
    try {
      await MeetingsService.Delete(meetingId);
      refresh();
      onClose();
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  return (
    <Modal label="Карточка встречи" onClose={onClose} dismissOnOverlay={!dirty}>
      {form && (
        <form
          className="card"
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          <div className="card__header">
            <label htmlFor="meeting-title" className="visually-hidden">Название встречи</label>
            <input
              id="meeting-title"
              className="card__title"
              type="text"
              placeholder="Название встречи"
              autoFocus={!meetingId}
              readOnly={!editable}
              value={form.title}
              onChange={(e) => set('title', e.target.value)}
            />
            <button type="button" className="icon-button card__close" aria-label="Закрыть карточку" onClick={onClose}>
              <CloseIcon />
            </button>
          </div>

          <div className="card__grid card__grid--3">
            <Field label="Дата">
              <DateField
                value={form.date}
                display={pickerDate(form.date)}
                ariaLabel="Дата"
                disabled={!editable}
                onChange={(v) => set('date', v)}
              />
            </Field>
            <Field label="Начало">
              <SelectField ariaLabel="Начало" value={form.start} options={timeOptions(form.start)} disabled={!editable} onChange={(v) => set('start', v)} />
            </Field>
            <Field label="Длительность">
              <SelectField ariaLabel="Длительность" value={form.durationMin} options={durations} disabled={!editable} onChange={(v) => set('durationMin', v)} />
            </Field>
          </div>

          <Field label="Повтор">
            <SelectField ariaLabel="Повтор" value={form.repeat} options={repeats} disabled={!editable} onChange={(v) => set('repeat', v)} />
          </Field>

          <Field label="Ссылки на созвон">
            <LinksEditor
              links={form.links}
              onChange={(links) => set('links', links)}
              onOpen={(l) => LinksService.OpenMeetingLink(l.id).catch(showError)}
              defaultTitle="Созвон"
            />
            <p className="field__hint">Первая ссылка показывается кнопкой «Подключиться» в агенде и в списке встреч.</p>
          </Field>

          <Field label="Заметка" htmlFor="meeting-note">
            <textarea
              id="meeting-note"
              className="text-input textarea"
              placeholder="Повестка, что подготовить"
              value={form.note}
              onChange={(e) => set('note', e.target.value)}
            />
          </Field>

          {error && <p className="field__error" role="alert">{error}</p>}

          <div className="card__footer">
            {meetingId ? (
              <button type="button" className="danger-button" onClick={remove} disabled={busy}>
                {confirmDelete ? (form.repeat ? 'Удалить всю серию?' : 'Точно удалить?') : 'Удалить встречу'}
              </button>
            ) : <span />}
            <button type="submit" className="primary-button" disabled={busy}>Готово</button>
          </div>
        </form>
      )}
    </Modal>
  );
}
