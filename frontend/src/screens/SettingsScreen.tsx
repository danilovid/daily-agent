import { useCallback, useEffect, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { SettingsService } from '../../bindings/daily-agent/internal/app';
import type { AgentSettingsView, AgentStatusView, ConnectionCheck } from '../../bindings/daily-agent/internal/app';
import { Field } from '../components/Fields';
import { CloseIcon, UpDownIcon } from '../components/Icons';
import { Modal } from '../components/Modal';
import { errorText } from '../lib/data';

type Tab = 'agent' | 'mcp' | 'plugins' | 'general';

const TABS: { value: Tab; label: string }[] = [
  { value: 'agent', label: 'Агент' },
  { value: 'mcp', label: 'MCP-серверы' },
  { value: 'plugins', label: 'Плагины' },
  { value: 'general', label: 'Общие' },
];

/** Экран «Настройки». Пока готов раздел «Агент». */
export function SettingsScreen() {
  const [tab, setTab] = useState<Tab>('agent');
  const current = TABS.find((t) => t.value === tab)!;
  return (
    <>
      <div className="screen-header drag-region">
        <div className="screen-header__title">
          <h2 className="screen-title">Настройки</h2>
        </div>
      </div>
      <nav aria-label="Разделы настроек" className="segmented settings-tabs">
        {TABS.map((t) => (
          <button
            key={t.value}
            type="button"
            aria-current={t.value === tab ? 'page' : undefined}
            className={`segmented__item${t.value === tab ? ' segmented__item--active' : ''}`}
            onClick={() => setTab(t.value)}
          >
            {t.label}
          </button>
        ))}
      </nav>
      {tab === 'agent' ? <AgentSettings /> : <p className="placeholder">Раздел «{current.label}» появится в следующих шагах.</p>}
    </>
  );
}

function describeState(s: AgentStatusView): { text: string; tone: 'online' | 'pending' | 'error' } {
  switch (s.state) {
    case 'ready':
      return { text: 'На связи', tone: 'online' };
    case 'starting':
      return { text: 'Запускается…', tone: 'pending' };
    case 'restarting':
      return { text: 'Перезапускается…', tone: 'pending' };
    case 'failed':
      return { text: 'Недоступен', tone: 'error' };
    default:
      return { text: 'Не запущен', tone: 'pending' };
  }
}

/** Время ответа: «12 мс» или «1,4 с». */
function duration(ms: number): string {
  if (ms < 1000) return `${ms} мс`;
  return `${(ms / 1000).toFixed(1).replace('.', ',')} с`;
}

type Check = { running: boolean; result: ConnectionCheck | null };
const idle: Check = { running: false, result: null };

/** Раздел «Агент»: OpenCode и модели. */
function AgentSettings() {
  const [view, setView] = useState<AgentSettingsView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [server, setServer] = useState<Check>(idle);
  const [model, setModel] = useState<Check>(idle);
  const [editing, setEditing] = useState(false);

  const load = useCallback(() => {
    SettingsService.Agent().then((v) => {
      setView(v);
      setError(null);
    }).catch((e) => setError(errorText(e)));
  }, []);

  // Состояние OpenCode меняется само: после запуска, падения, смены модели.
  // Итоги прошлых проверок после этого устарели.
  useEffect(() => {
    load();
    const off = Events.On('agent:status', () => {
      setServer(idle);
      setModel(idle);
      load();
    });
    return () => off();
  }, [load]);

  const checkServer = () => {
    setServer({ running: true, result: null });
    SettingsService.CheckConnection()
      .then((r) => setServer({ running: false, result: r }))
      .catch((e) => setServer({ running: false, result: { ok: false, error: errorText(e), version: '', millis: 0, checkedAt: '' } }));
  };
  const checkModel = () => {
    setModel({ running: true, result: null });
    SettingsService.CheckModel()
      .then((r) => setModel({ running: false, result: r }))
      .catch((e) => setModel({ running: false, result: { ok: false, error: errorText(e), version: '', millis: 0, checkedAt: '' } }));
  };

  if (!view) return error ? <p className="settings-note settings-row__hint--error" role="alert">{error}</p> : null;

  const st = view.status;
  const state = describeState(st);
  let serverHint: { text: string; tone?: 'ok' | 'error' } | null = null;
  if (server.running) serverHint = { text: 'Проверяю…' };
  else if (server.result?.ok) serverHint = { text: `Ответил за ${duration(server.result.millis)} · ${server.result.checkedAt}`, tone: 'ok' };
  else if (server.result) serverHint = { text: server.result.error, tone: 'error' };
  else if (st.state === 'failed' && st.error) serverHint = { text: st.error, tone: 'error' };
  else if (st.state === 'ready' && st.since) serverHint = { text: `С ${st.since}` };

  const m = view.model;
  const modelReady = st.state === 'ready' && st.modelConfigured;
  let modelHint: { text: string; tone?: 'ok' | 'error' };
  if (model.running) modelHint = { text: 'Жду ответа модели…' };
  else if (model.result?.ok) modelHint = { text: `Ответила за ${duration(model.result.millis)} · ${model.result.checkedAt}`, tone: 'ok' };
  else if (model.result) modelHint = { text: model.result.error, tone: 'error' };
  else modelHint = { text: 'Короткий вопрос через OpenCode. Текст ответа не показывается' };

  const modelName = m.source === 'stub' ? 'Заглушка для разработки' : m.modelId || 'Не настроена';

  return (
    <>
      <section className="settings-section" aria-labelledby="settings-opencode">
        <h3 className="settings-section__title" id="settings-opencode">OpenCode</h3>
        <div className="settings-group">
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Состояние</span>
              {serverHint && (
                <span className={`settings-row__hint${serverHint.tone ? ` settings-row__hint--${serverHint.tone}` : ''}`} aria-live="polite">
                  {serverHint.text}
                </span>
              )}
            </div>
            <span className="settings-row__state">
              <span className={`settings-row__dot settings-row__dot--${state.tone}`} aria-hidden="true" />
              {state.text}
            </span>
            <button type="button" className="settings-action" onClick={checkServer} disabled={server.running}>
              Проверить связь
            </button>
          </div>
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Запуск</span>
              <span className="settings-row__hint">Приложение само запускает и останавливает OpenCode</span>
            </div>
            <span className="settings-row__value settings-row__value--plain">Вместе с приложением</span>
          </div>
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Программа</span>
              <span className="settings-row__hint">Версия {view.pinnedVersion}</span>
            </div>
            <span className="settings-row__value" title={view.binary}>{view.binary || 'Не найдена'}</span>
          </div>
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Рабочая папка</span>
              <span className="settings-row__hint">Команды агента и его настройки</span>
            </div>
            <span className="settings-row__value" title={view.workspace}>{view.workspace}</span>
          </div>
        </div>
      </section>

      <section className="settings-section" aria-labelledby="settings-models">
        <h3 className="settings-section__title" id="settings-models">Модели</h3>
        <div className="settings-group">
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Для рабочих задач</span>
              <span className="settings-row__hint">Только селфхост: рабочие данные не уходят в облако</span>
            </div>
            <button type="button" className="settings-select" aria-label={`Модель для рабочих задач: ${modelName}`} onClick={() => setEditing(true)}>
              <span>{modelName}</span>
              <UpDownIcon size="var(--icon-xs)" />
            </button>
          </div>
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Для личных задач</span>
              <span className="settings-row__hint">Отдельная облачная модель появится позже</span>
            </div>
            <span className="settings-row__value settings-row__value--plain">Та же, что для рабочих</span>
          </div>
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Селфхост-API</span>
              <span className="settings-row__hint">{m.keySaved ? 'Ключ хранится в Связке ключей' : 'Без ключа'}</span>
            </div>
            <span className="settings-row__value" title={m.url}>{m.url || 'Не задан'}</span>
            <button type="button" className="settings-action" onClick={() => setEditing(true)}>Изменить</button>
          </div>
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Связь с моделью</span>
              <span className={`settings-row__hint${modelHint.tone ? ` settings-row__hint--${modelHint.tone}` : ''}`} aria-live="polite">
                {modelHint.text}
              </span>
            </div>
            <button type="button" className="settings-action" onClick={checkModel} disabled={model.running || !modelReady}>
              Проверить модель
            </button>
          </div>
        </div>
      </section>

      {view.note && <p className="settings-note settings-note--warning">{view.note}</p>}
      {error && <p className="settings-note settings-row__hint--error" role="alert">{error}</p>}
      <p className="settings-note">
        У первой и второй версии OpenCode разный формат настроек. Приложение работает с одной закреплённой версией и при запуске проверяет, что установлена именно она.
      </p>

      {editing && (
        <ModelDialog
          view={view}
          onClose={() => setEditing(false)}
          onSaved={(v) => {
            setView(v);
            setServer(idle);
            setModel(idle);
            setEditing(false);
          }}
        />
      )}
    </>
  );
}

type DialogProps = {
  view: AgentSettingsView;
  onClose: () => void;
  onSaved: (view: AgentSettingsView) => void;
};

/** Окно «Селфхост-модель»: адрес API, модель и ключ. Ключ уходит в Связку ключей. */
function ModelDialog({ view, onClose, onSaved }: DialogProps) {
  const m = view.model;
  const [url, setUrl] = useState(m.source === 'stub' ? '' : m.url);
  const [modelId, setModelId] = useState(m.source === 'stub' ? '' : m.modelId);
  const [key, setKey] = useState('');
  const [removeKey, setRemoveKey] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dirty = key !== '' || removeKey || url !== m.url || modelId !== m.modelId;

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      const v = await SettingsService.SaveModel({ url, modelId, key, removeKey });
      setKey('');
      onSaved(v);
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  return (
    <Modal label="Селфхост-модель" onClose={onClose} dismissOnOverlay={!dirty}>
      <form
        className="card"
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <div className="card__header">
          <h2 className="card__heading model-form__title">Селфхост-модель</h2>
          <button type="button" className="icon-button card__close" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon />
          </button>
        </div>

        <Field label="Адрес API" htmlFor="model-url">
          <input
            id="model-url"
            className="text-input"
            type="url"
            placeholder="http://127.0.0.1:8000/v1"
            value={url}
            autoFocus
            autoComplete="off"
            spellCheck={false}
            onChange={(e) => { setUrl(e.target.value); setError(null); }}
          />
          <p className="field__hint">API, совместимый с OpenAI. Облачные сервисы для рабочих задач не принимаются.</p>
        </Field>

        <Field label="Модель" htmlFor="model-id">
          <input
            id="model-id"
            className="text-input"
            type="text"
            placeholder="Идентификатор модели"
            value={modelId}
            autoComplete="off"
            spellCheck={false}
            onChange={(e) => { setModelId(e.target.value); setError(null); }}
          />
        </Field>

        <Field label="Ключ API" htmlFor="model-key">
          <div className="model-form__key">
            <input
              id="model-key"
              className="text-input"
              type="password"
              placeholder={removeKey ? 'Ключ будет удалён' : m.keySaved ? 'Сохранён. Оставьте пустым, чтобы не менять' : 'Если API требует ключ'}
              value={key}
              disabled={removeKey}
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => { setKey(e.target.value); setError(null); }}
            />
            {m.keySaved && (
              <button type="button" className="text-button" onClick={() => { setRemoveKey((r) => !r); setKey(''); }}>
                {removeKey ? 'Не удалять' : 'Удалить ключ'}
              </button>
            )}
          </div>
          <p className="field__hint">Ключ хранится в Связке ключей macOS и не попадает в файлы настроек.</p>
        </Field>

        {error && <p className="field__error" role="alert">{error}</p>}

        <div className="card__footer">
          <p className="field__hint">После сохранения OpenCode перезапустится с новой моделью.</p>
          <div className="model-form__actions">
            <button type="button" className="secondary-button" onClick={onClose} disabled={busy}>Отмена</button>
            <button type="submit" className="primary-button" disabled={busy}>{busy ? 'Сохраняю…' : 'Сохранить'}</button>
          </div>
        </div>
      </form>
    </Modal>
  );
}
