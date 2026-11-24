import { useEffect, useState } from 'react';
import { TagsService } from '../../bindings/daily-agent/internal/app';
import type { TagView } from '../../bindings/daily-agent/internal/app';
import { errorText } from '../lib/data';
import { PlusIcon, SmallCheckIcon } from './Icons';

type NewTagFormProps = {
  onCreated: (tag: TagView) => void;
  onCancel: () => void;
};

/** Форма нового тега: название и цвет из палитры. */
export function NewTagForm({ onCreated, onCancel }: NewTagFormProps) {
  const [palette, setPalette] = useState<string[]>([]);
  const [name, setName] = useState('');
  const [color, setColor] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    TagsService.Palette().then((p) => {
      setPalette(p ?? []);
      setColor((c) => c || (p?.[0] ?? ''));
    });
  }, []);

  const create = async () => {
    setBusy(true);
    try {
      const tag = await TagsService.Create(name, color);
      onCreated(tag);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="new-tag">
      <div className="new-tag__row">
        <label htmlFor="new-tag-name" className="visually-hidden">Название тега</label>
        <input
          id="new-tag-name"
          className="text-input new-tag__name"
          type="text"
          placeholder="Название тега"
          maxLength={32}
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              create();
            }
          }}
        />
        <button type="button" className="secondary-button" onClick={create} disabled={busy}>Создать</button>
        <button type="button" className="text-button" onClick={onCancel}>Отмена</button>
      </div>
      <div className="new-tag__palette" role="radiogroup" aria-label="Цвет тега">
        {palette.map((c) => (
          <button
            key={c}
            type="button"
            role="radio"
            aria-checked={c === color}
            aria-label={`Цвет ${c}`}
            className={`swatch${c === color ? ' swatch--selected' : ''}`}
            style={{ background: c }}
            onClick={() => setColor(c)}
          />
        ))}
      </div>
      {error && <p className="field__error" role="alert">{error}</p>}
    </div>
  );
}

type TagPickerProps = {
  tags: TagView[];
  selected: string[];
  onChange: (ids: string[]) => void;
  onTagCreated: (tag: TagView) => void;
};

/** Теги в карточке задачи: нажатие ставит или снимает тег. */
export function TagPicker({ tags, selected, onChange, onTagCreated }: TagPickerProps) {
  const [creating, setCreating] = useState(false);
  const toggle = (id: string) =>
    onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id]);

  return (
    <>
      <div className="tag-picker">
        {tags.map((t) => {
          const on = selected.includes(t.id);
          return (
            <button key={t.id} type="button" className="tag-button" aria-pressed={on} aria-label={`Тег ${t.name}`} onClick={() => toggle(t.id)}>
              <span className={`tag-chip tag-chip--sidebar${on ? ' tag-chip--selected' : ' tag-chip--off'}`}>
                <span className="tag-dot" style={{ background: t.color }} aria-hidden="true" />
                {t.name}
                {on && <SmallCheckIcon />}
              </span>
            </button>
          );
        })}
        {!creating && (
          <button type="button" className="text-button text-button--accent" onClick={() => setCreating(true)}>
            <PlusIcon size="var(--icon-sm)" />
            <span>Новый тег</span>
          </button>
        )}
      </div>
      {creating && (
        <NewTagForm
          onCancel={() => setCreating(false)}
          onCreated={(tag) => {
            setCreating(false);
            onTagCreated(tag);
            onChange([...selected, tag.id]);
          }}
        />
      )}
    </>
  );
}
