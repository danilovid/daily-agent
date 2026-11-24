import { useCallback, useEffect, useRef, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { MCPService } from '../../bindings/daily-agent/internal/app';
import type { MCPServerView, MCPTokenView } from '../../bindings/daily-agent/internal/app';
import { Field } from '../components/Fields';
import { CloseIcon, MoreIcon, PlusIcon } from '../components/Icons';
import { Modal } from '../components/Modal';
import { Segmented } from '../components/Segmented';
import { errorText } from '../lib/data';

const STATUS: Record<string, { text: string; tone: 'online' | 'warning' | 'error' | 'off' }> = {
  connected: { text: 'Подключён', tone: 'online' },
  needs_auth: { text: 'Нужен вход', tone: 'warning' },
  needs_token: { text: 'Нужен токен', tone: 'warning' },
  failed: { text: 'Ошибка', tone: 'error' },
  disabled: { text: 'Выключен', tone: 'off' },
  pending: { text: 'Подключается…', tone: 'off' },
  stopped: { text: 'OpenCode не на связи', tone: 'off' },
};

type Dialog =
  | { kind: 'add' }
  | { kind: 'tools'; server: MCPServerView }
  | { kind: 'token'; server: MCPServerView };

/** Раздел «MCP-серверы»: серверы агента, их состояние, инструменты и вход. */
export function McpSettings() {
  const [servers, setServers] = useState<MCPServerView[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [dialog, setDialog] = useState<Dialog | null>(null);

  const load = useCallback(() => {
    MCPService.List()
      .then((list) => {
        setServers(list ?? []);
        setError(null);
      })
      .catch((e) => setError(errorText(e)));
  }, []);

  // Состояние серверов меняется с перезапуском OpenCode и после входа.
  useEffect(() => {
    load();
    const off = Events.On('agent:status', () => load());
    return () => off();
  }, [load]);

  const run = (name: string, action: () => Promise<MCPServerView[] | null | void>) => {
    setBusy(name);
    setError(null);
    action()
      .then((list) => {
        if (Array.isArray(list)) setServers(list);
        else load();
      })
      .catch((e) => setError(errorText(e)))
      .finally(() => setBusy(null));
  };

  const applied = (list: MCPServerView[] | null) => {
    setServers(list ?? []);
    setDialog(null);
  };

  return (
    <>
      <div className="mcp-intro">
        <p className="settings-note mcp-intro__text">Серверы подключаются к агенту. Приложение записывает их в настройки OpenCode.</p>
        <button type="button" className="secondary-button mcp-intro__add" onClick={() => setDialog({ kind: 'add' })}>
          <PlusIcon size="var(--icon-md)" />
          <span>Сервер</span>
        </button>
      </div>

      {servers && servers.length === 0 && (
        <p className="settings-note">
          Серверов пока нет. Добавьте сервер или опишите его в настройках OpenCode (~/.config/opencode) — приложение подхватит его само.
        </p>
      )}
      {servers && servers.length > 0 && (
        <div className="settings-group">
          {servers.map((s) => (
            <ServerRow
              key={s.name}
              server={s}
              busy={busy === s.name}
              onToggle={() => run(s.name, () => MCPService.SetEnabled(s.name, !s.enabled))}
              onLogin={() => run(s.name, () => MCPService.Login(s.name))}
              onRefresh={() => run(s.name, () => MCPService.Refresh())}
              onRemove={() => run(s.name, () => MCPService.Remove(s.name))}
              onTools={() => setDialog({ kind: 'tools', server: s })}
              onToken={() => setDialog({ kind: 'token', server: s })}
            />
          ))}
        </div>
      )}
      {error && <p className="settings-note settings-row__hint--error" role="alert">{error}</p>}

      <section className="settings-section" aria-labelledby="settings-rights">
        <h3 className="settings-section__title" id="settings-rights">Права агента</h3>
        <div className="settings-group">
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Рабочие системы</span>
              <span className="settings-row__hint">Агент читает данные и ничего не отправляет. Инструменты записи закрыты</span>
            </div>
            <span className="settings-row__value settings-row__value--plain">Только чтение</span>
          </div>
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Личные чаты</span>
              <span className="settings-row__hint">Личные чаты могут идти в облачную модель, поэтому серверы им недоступны</span>
            </div>
            <span className="settings-row__value settings-row__value--plain">Без доступа</span>
          </div>
        </div>
      </section>

      {dialog?.kind === 'add' && <AddDialog onClose={() => setDialog(null)} onSaved={applied} />}
      {dialog?.kind === 'tools' && <ToolsDialog server={dialog.server} onClose={() => setDialog(null)} onSaved={applied} />}
      {dialog?.kind === 'token' && <TokenDialog server={dialog.server} onClose={() => setDialog(null)} onSaved={applied} />}
    </>
  );
}

type RowProps = {
  server: MCPServerView;
  busy: boolean;
  onToggle: () => void;
  onLogin: () => void;
  onRefresh: () => void;
  onRemove: () => void;
  onTools: () => void;
  onToken: () => void;
};

function ServerRow({ server: s, busy, onToggle, onLogin, onRefresh, onRemove, onTools, onToken }: RowProps) {
  const status = STATUS[s.status] ?? { text: s.status, tone: 'off' as const };
  const kind = s.type === 'local' ? 'Локальный' : 'Удалённый';
  const source = s.source === 'opencode' ? ' · из настроек OpenCode' : '';
  const tools = s.tools ?? [];
  const allowed = tools.filter((t) => t.allowed).length;
  return (
    <div className="mcp-row">
      <span className={`settings-row__dot mcp-row__dot--${status.tone}`} aria-hidden="true" />
      <div className="mcp-row__label">
        <span className="mcp-row__title">{s.title}</span>
        <span className="mcp-row__hint" title={s.address}>{kind} · {s.address}{source}</span>
        {s.status === 'connected' && (
          <span className="mcp-row__hint">Разрешено инструментов: {allowed} из {tools.length}</span>
        )}
        {s.status === 'failed' && s.error && (
          <span className="mcp-row__hint mcp-row__hint--error" title={s.error}>{s.error}</span>
        )}
      </div>
      <span className="mcp-row__pill">Только чтение</span>
      <span className={`mcp-row__status mcp-row__status--${status.tone}`} title={s.error || undefined}>{status.text}</span>
      {s.status === 'needs_auth' && (
        <button type="button" className="settings-action" onClick={onLogin} disabled={busy}>Войти</button>
      )}
      {s.status === 'needs_token' && (
        <button type="button" className="settings-action" onClick={onToken} disabled={busy}>Токен…</button>
      )}
      <button
        type="button"
        role="switch"
        aria-checked={s.enabled}
        aria-label={`Сервер включён: ${s.title}`}
        className="mcp-switch"
        onClick={onToggle}
        disabled={busy}
      >
        <span className={`mcp-switch__track${s.enabled ? ' mcp-switch__track--on' : ''}`} aria-hidden="true">
          <span className="mcp-switch__knob" />
        </span>
      </button>
      <ServerMenu server={s} onRefresh={onRefresh} onRemove={onRemove} onTools={onTools} onToken={onToken} />
    </div>
  );
}

type MenuProps = { server: MCPServerView; onRefresh: () => void; onRemove: () => void; onTools: () => void; onToken: () => void };

function ServerMenu({ server: s, onRefresh, onRemove, onTools, onToken }: MenuProps) {
  const [open, setOpen] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) {
        setOpen(false);
        setConfirm(false);
      }
    };
    window.addEventListener('mousedown', close);
    return () => window.removeEventListener('mousedown', close);
  }, [open]);
  const pick = (fn: () => void) => () => {
    setOpen(false);
    fn();
  };
  return (
    <div className="chat-menu" ref={root}>
      <button type="button" className="icon-button" aria-label={`Действия с сервером: ${s.title}`} aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        <MoreIcon />
      </button>
      {open && (
        <div className="chat-menu__popover" role="menu">
          <button role="menuitem" className="chat-menu__item" onClick={pick(onTools)} disabled={(s.tools ?? []).length === 0}>Инструменты…</button>
          {(s.tokens ?? []).length > 0 && <button role="menuitem" className="chat-menu__item" onClick={pick(onToken)}>Токен…</button>}
          <button role="menuitem" className="chat-menu__item" onClick={pick(onRefresh)}>Обновить</button>
          {s.source === 'app' && (
            <button role="menuitem" className="chat-menu__item chat-menu__item--danger" onClick={() => (confirm ? pick(onRemove)() : setConfirm(true))}>
              {confirm ? 'Точно удалить?' : 'Удалить сервер'}
            </button>
          )}
        </div>
      )}
    </div>
  );
}

type DialogProps = { onClose: () => void; onSaved: (list: MCPServerView[] | null) => void };

function DialogHeader({ title, onClose }: { title: string; onClose: () => void }) {
  return (
    <div className="card__header">
      <h2 className="card__heading model-form__title">{title}</h2>
      <button type="button" className="icon-button card__close" aria-label="Закрыть" onClick={onClose}>
        <CloseIcon />
      </button>
    </div>
  );
}

/** Новый MCP-сервер по макету settings-mcp-add.html. */
function AddDialog({ onClose, onSaved }: DialogProps) {
  const [title, setTitle] = useState('');
  const [type, setType] = useState<'remote' | 'local'>('remote');
  const [url, setUrl] = useState('');
  const [command, setCommand] = useState('');
  const [auth, setAuth] = useState<'oauth' | 'token' | 'none'>('oauth');
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dirty = title !== '' || url !== '' || command !== '' || token !== '';

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      const list = await MCPService.Add({ title, type, url, command, auth: type === 'local' ? 'none' : auth, token });
      setToken('');
      onSaved(list);
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  return (
    <Modal label="Новый MCP-сервер" onClose={onClose} dismissOnOverlay={!dirty}>
      <form className="card" onSubmit={(e) => { e.preventDefault(); save(); }}>
        <DialogHeader title="Новый MCP-сервер" onClose={onClose} />
        <Field label="Название" htmlFor="mcp-name">
          <input id="mcp-name" className="text-input" type="text" placeholder="Например, Трекер задач" value={title} autoFocus
            onChange={(e) => { setTitle(e.target.value); setError(null); }} />
        </Field>
        <Field label="Тип">
          <Segmented
            label="Тип сервера"
            value={type}
            options={[{ value: 'remote', label: 'Удалённый: адрес' }, { value: 'local', label: 'Локальный: команда' }]}
            onChange={setType}
          />
        </Field>
        {type === 'remote' ? (
          <Field label="Адрес сервера" htmlFor="mcp-url">
            <input id="mcp-url" className="text-input" type="url" placeholder="https://…" value={url} autoComplete="off" spellCheck={false}
              onChange={(e) => { setUrl(e.target.value); setError(null); }} />
          </Field>
        ) : (
          <Field label="Команда запуска" htmlFor="mcp-command">
            <input id="mcp-command" className="text-input" type="text" placeholder="Например, npx -y my-mcp-server" value={command} autoComplete="off" spellCheck={false}
              onChange={(e) => { setCommand(e.target.value); setError(null); }} />
            <p className="field__hint">Команду запускает OpenCode в рабочей папке агента.</p>
          </Field>
        )}
        {type === 'remote' && (
          <Field label="Вход">
            <Segmented
              label="Способ входа"
              value={auth}
              options={[{ value: 'oauth', label: 'Через браузер (OAuth)' }, { value: 'token', label: 'Токен' }, { value: 'none', label: 'Без входа' }]}
              onChange={setAuth}
            />
            {auth === 'oauth' && <p className="field__hint">Вход откроется в браузере. Данные входа хранит OpenCode, вне папки с настройками.</p>}
            {auth === 'token' && (
              <input className="text-input" type="password" aria-label="Токен" placeholder="Токен доступа" value={token} autoComplete="off" spellCheck={false}
                onChange={(e) => { setToken(e.target.value); setError(null); }} />
            )}
            {auth === 'token' && <p className="field__hint">Токен хранится в Связке ключей macOS и уходит серверу заголовком Authorization.</p>}
          </Field>
        )}
        <Field label="Доступ">
          <div role="group" aria-label="Доступ" className="segmented">
            <button type="button" aria-pressed="true" className="segmented__item segmented__item--active">Только чтение</button>
            <button type="button" aria-pressed="false" className="segmented__item" disabled>Чтение и запись</button>
          </div>
          <p className="field__hint">Запись в рабочие системы появится в v0.2 и только после подтверждения.</p>
        </Field>
        {error && <p className="field__error" role="alert">{error}</p>}
        <div className="card__footer">
          <span />
          <div className="model-form__actions">
            <button type="button" className="secondary-button" onClick={onClose} disabled={busy}>Отмена</button>
            <button type="submit" className="primary-button" disabled={busy}>{busy ? 'Проверяю…' : 'Проверить и добавить'}</button>
          </div>
        </div>
      </form>
    </Modal>
  );
}

/** Инструменты сервера: какие разрешены агенту. Запись выбрать нельзя. */
function ToolsDialog({ server: s, onClose, onSaved }: DialogProps & { server: MCPServerView }) {
  const tools = s.tools ?? [];
  const [picked, setPicked] = useState<Set<string>>(() => new Set(tools.filter((t) => t.allowed).map((t) => t.name)));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const save = async (reset: boolean) => {
    setBusy(true);
    setError(null);
    try {
      onSaved(await MCPService.SetTools(s.name, [...picked], reset));
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  return (
    <Modal label={`Инструменты: ${s.title}`} onClose={onClose}>
      <form className="card" onSubmit={(e) => { e.preventDefault(); save(false); }}>
        <DialogHeader title={`Инструменты: ${s.title}`} onClose={onClose} />
        <p className="field__hint">
          {s.custom ? 'Инструменты выбраны вручную.' : 'По умолчанию разрешены инструменты чтения: get, list, search и подобные.'} Инструменты записи в этой версии закрыты.
        </p>
        <ul className="mcp-tools">
          {tools.map((t) => (
            <li key={t.name} className="mcp-tools__item">
              <label className="mcp-tools__label">
                <input
                  type="checkbox"
                  checked={picked.has(t.name) && !t.write}
                  disabled={t.write}
                  onChange={(e) => {
                    const next = new Set(picked);
                    if (e.target.checked) next.add(t.name);
                    else next.delete(t.name);
                    setPicked(next);
                  }}
                />
                <span className="mcp-tools__text">
                  <span className="mcp-tools__name">{t.short}{t.write && <span className="mcp-tools__tag">запись</span>}</span>
                  {t.description && <span className="mcp-tools__desc">{t.description}</span>}
                </span>
              </label>
            </li>
          ))}
        </ul>
        {error && <p className="field__error" role="alert">{error}</p>}
        <div className="card__footer">
          <button type="button" className="text-button" onClick={() => save(true)} disabled={busy || !s.custom}>Только чтение по умолчанию</button>
          <div className="model-form__actions">
            <button type="button" className="secondary-button" onClick={onClose} disabled={busy}>Отмена</button>
            <button type="submit" className="primary-button" disabled={busy}>{busy ? 'Сохраняю…' : 'Сохранить'}</button>
          </div>
        </div>
      </form>
    </Modal>
  );
}

/** Секреты сервера: значения заголовков или переменных окружения. */
function TokenDialog({ server: s, onClose, onSaved }: DialogProps & { server: MCPServerView }) {
  const tokens = s.tokens ?? [];
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const id = (t: MCPTokenView) => `${t.kind}:${t.key}`;

  const importFromConfig = async () => {
    setBusy(true);
    setError(null);
    try {
      onSaved(await MCPService.ImportTokens(s.name));
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      let list: MCPServerView[] | null = null;
      for (const t of tokens) {
        const v = values[id(t)]?.trim();
        if (v) list = await MCPService.SetToken(s.name, t.kind, t.key, v);
      }
      setValues({});
      onSaved(list);
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  return (
    <Modal label={`Токен: ${s.title}`} onClose={onClose} dismissOnOverlay={Object.keys(values).length === 0}>
      <form className="card" onSubmit={(e) => { e.preventDefault(); save(); }}>
        <DialogHeader title={`Токен: ${s.title}`} onClose={onClose} />
        {tokens.map((t, i) => (
          <Field key={id(t)} label={t.kind === 'header' ? `Заголовок ${t.key}` : `Переменная ${t.key}`} htmlFor={`mcp-token-${i}`}>
            <input
              id={`mcp-token-${i}`}
              className="text-input"
              type="password"
              placeholder={t.saved ? 'Сохранён. Введите новый, чтобы заменить' : 'Токен'}
              value={values[id(t)] ?? ''}
              autoFocus={i === 0}
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => { setValues({ ...values, [id(t)]: e.target.value }); setError(null); }}
            />
          </Field>
        ))}
        <p className="field__hint">Токены хранятся в Связке ключей macOS. В настройках OpenCode агента на их месте только ссылка на переменную окружения.</p>
        {s.tokensInConfig && (
          <p className="field__hint">Токен записан в ваших настройках OpenCode. Его можно перенести в Связку ключей одной кнопкой.</p>
        )}
        {error && <p className="field__error" role="alert">{error}</p>}
        <div className="card__footer">
          {s.tokensInConfig ? (
            <button type="button" className="text-button text-button--accent" onClick={importFromConfig} disabled={busy}>
              Взять из настроек OpenCode
            </button>
          ) : <span />}
          <div className="model-form__actions">
            <button type="button" className="secondary-button" onClick={onClose} disabled={busy}>Отмена</button>
            <button type="submit" className="primary-button" disabled={busy || Object.values(values).every((v) => !v.trim())}>
              {busy ? 'Сохраняю…' : 'Сохранить'}
            </button>
          </div>
        </div>
      </form>
    </Modal>
  );
}
