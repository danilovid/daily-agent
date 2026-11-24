import { useState } from 'react';
import { CloseIcon, ExternalIcon } from './Icons';

export type EditableLink = { id: string; title: string; url: string };

type Props = {
  links: EditableLink[];
  onChange: (links: EditableLink[]) => void;
  /** Открыть сохранённую ссылку. У ещё не сохранённой ссылки нет id. */
  onOpen: (link: EditableLink) => void;
  /** Ссылка на источник внешней записи: её нельзя удалить. */
  source?: { label: string; url: string; onOpen: () => void };
  defaultTitle: string;
  disabled?: boolean;
};

const URL_PATTERN = /^https?:\/\/[^\s/$.?#][^\s]*$/i;

/** Список ссылок с кнопкой удаления и форма новой ссылки. */
export function LinksEditor({ links, onChange, onOpen, source, defaultTitle, disabled }: Props) {
  const [title, setTitle] = useState('');
  const [url, setUrl] = useState('');
  const [error, setError] = useState<string | null>(null);

  const add = () => {
    const u = url.trim();
    if (!URL_PATTERN.test(u)) {
      setError('Нужна ссылка вида https://…');
      return;
    }
    onChange([...links, { id: '', title: title.trim() || defaultTitle, url: u }]);
    setTitle('');
    setUrl('');
    setError(null);
  };

  return (
    <>
      {source && (
        <div className="link-row">
          <button type="button" className="link-chip" onClick={source.onOpen} aria-label={`Открыть в источнике: ${source.label}`}>
            <span className="link-chip__body link-chip__body--card"><ExternalIcon />{source.label}</span>
          </button>
          <span className="link-row__url">{source.url}</span>
        </div>
      )}
      {links.map((l, i) => (
        <div className="link-row" key={l.id || `new-${i}`}>
          <button
            type="button"
            className="link-chip"
            disabled={!l.id}
            title={l.id ? undefined : 'Ссылка откроется после сохранения'}
            onClick={() => l.id && onOpen(l)}
            aria-label={`Открыть ссылку: ${l.title}`}
          >
            <span className="link-chip__body link-chip__body--card"><ExternalIcon />{l.title}</span>
          </button>
          <span className="link-row__url">{l.url}</span>
          {!disabled && (
            <button
              type="button"
              className="icon-button link-row__remove"
              aria-label={`Удалить ссылку ${l.title}`}
              onClick={() => onChange(links.filter((_, j) => j !== i))}
            >
              <CloseIcon />
            </button>
          )}
        </div>
      ))}
      {!disabled && (
        <div className="link-form">
          <label htmlFor="link-name" className="visually-hidden">Название кнопки</label>
          <input
            id="link-name"
            className="text-input link-form__name"
            type="text"
            placeholder="Название кнопки"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
          <label htmlFor="link-url" className="visually-hidden">Адрес ссылки</label>
          <input
            id="link-url"
            className="text-input link-form__url"
            type="url"
            placeholder="https://…"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                add();
              }
            }}
            spellCheck={false}
            autoCapitalize="off"
          />
          <button type="button" className="secondary-button" onClick={add}>Добавить</button>
        </div>
      )}
      {error && <p className="field__error" role="alert">{error}</p>}
    </>
  );
}
