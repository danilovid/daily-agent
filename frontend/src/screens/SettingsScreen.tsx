import { useCallback, useEffect, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { SettingsService } from '../../bindings/daily-agent/internal/app';
import type {
  AgentSettingsView,
  AgentStatusView,
  CatalogOptionView,
  ConnectionCheck,
  ModelOptionView,
  ProviderView,
} from '../../bindings/daily-agent/internal/app';
import { Field } from '../components/Fields';
import { CloseIcon, UpDownIcon } from '../components/Icons';
import { Modal } from '../components/Modal';
import { errorText } from '../lib/data';
import { McpSettings } from './McpSettings';

type Tab = 'agent' | 'mcp' | 'plugins' | 'general';

const TABS: { value: Tab; label: string }[] = [
  { value: 'agent', label: 'Агент' },
  { value: 'mcp', label: 'MCP-серверы' },
  { value: 'plugins', label: 'Плагины' },
  { value: 'general', label: 'Общие' },
];

/** Экран «Настройки». Готовы разделы «Агент» и «MCP-серверы». */
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
      {tab === 'agent' && <AgentSettings />}
      {tab === 'mcp' && <McpSettings />}
      {tab !== 'agent' && tab !== 'mcp' && <p className="placeholder">Раздел «{current.label}» появится в следующих шагах.</p>}
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
const failed = (e: unknown): ConnectionCheck => ({ ok: false, error: errorText(e), version: '', millis: 0, checkedAt: '' });

type Hint = { text: string; tone?: 'ok' | 'error' };

function checkHint(label: string, c: Check): Hint | null {
  if (c.running) return { text: `${label}: жду ответа…` };
  if (c.result?.ok) return { text: `${label}: ответила за ${duration(c.result.millis)} · ${c.result.checkedAt}`, tone: 'ok' };
  if (c.result) return { text: `${label}: ${c.result.error}`, tone: 'error' };
  return null;
}

type Dialog = { kind: 'own' } | { kind: 'key'; provider: ProviderView } | { kind: 'connect' };

/** Раздел «Агент»: OpenCode, модели и провайдеры. */
function AgentSettings() {
  const [view, setView] = useState<AgentSettingsView | null>(null);
  const [models, setModels] = useState<ModelOptionView[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [server, setServer] = useState<Check>(idle);
  const [workCheck, setWorkCheck] = useState<Check>(idle);
  const [personalCheck, setPersonalCheck] = useState<Check>(idle);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [saving, setSaving] = useState(false);

  // Список моделей даёт OpenCode, поэтому он читается, когда тот на связи.
  const load = useCallback(() => {
    SettingsService.Agent().then((v) => {
      setView(v);
      setError(null);
      if (v.status.state === 'ready') {
        SettingsService.Models().then((list) => setModels(list ?? [])).catch(() => setModels(null));
      } else {
        setModels(null);
      }
    }).catch((e) => setError(errorText(e)));
  }, []);

  // Состояние OpenCode меняется само: после запуска, падения, смены провайдеров.
  // Итоги прошлых проверок после этого устарели.
  useEffect(() => {
    load();
    const off = Events.On('agent:status', () => {
      setServer(idle);
      setWorkCheck(idle);
      setPersonalCheck(idle);
      load();
    });
    return () => off();
  }, [load]);

  const applied = (v: AgentSettingsView) => {
    setView(v);
    setDialog(null);
    setWorkCheck(idle);
    setPersonalCheck(idle);
    load();
  };

  const checkServer = () => {
    setServer({ running: true, result: null });
    SettingsService.CheckConnection()
      .then((r) => setServer({ running: false, result: r }))
      .catch((e) => setServer({ running: false, result: failed(e) }));
  };
  const checkModel = (personal: boolean) => {
    const set = personal ? setPersonalCheck : setWorkCheck;
    set({ running: true, result: null });
    SettingsService.CheckModel(personal)
      .then((r) => set({ running: false, result: r }))
      .catch((e) => set({ running: false, result: failed(e) }));
  };
  const chooseModels = (work: string, personal: string) => {
    setSaving(true);
    SettingsService.SaveModels({ work, personal })
      .then((v) => {
        setError(null);
        applied(v);
      })
      .catch((e) => setError(errorText(e)))
      .finally(() => setSaving(false));
  };

  if (!view) return error ? <p className="settings-note settings-row__hint--error" role="alert">{error}</p> : null;

  const st = view.status;
  const state = describeState(st);
  let serverHint: Hint | null = null;
  if (server.running) serverHint = { text: 'Проверяю…' };
  else if (server.result?.ok) serverHint = { text: `Ответил за ${duration(server.result.millis)} · ${server.result.checkedAt}`, tone: 'ok' };
  else if (server.result) serverHint = { text: server.result.error, tone: 'error' };
  else if (st.state === 'failed' && st.error) serverHint = { text: st.error, tone: 'error' };
  else if (st.state === 'ready' && st.since) serverHint = { text: `С ${st.since}` };

  const m = view.model;
  const stub = m.source === 'stub';
  const ready = st.state === 'ready';
  const checks = [checkHint('Рабочая', workCheck), checkHint('Личная', personalCheck)].filter((h): h is Hint => h !== null);
  const providers = view.providers ?? [];
  const own = providers.find((p) => p.source === 'own');
  const imported = providers.filter((p) => p.source === 'opencode');
  const catalog = providers.filter((p) => p.source === 'catalog');

  return (
    <>
      <section className="settings-section" aria-labelledby="settings-opencode">
        <h3 className="settings-section__title" id="settings-opencode">OpenCode</h3>
        <div className="settings-group">
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Состояние</span>
              {serverHint && <RowHint hint={serverHint} />}
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
            {stub ? (
              <span className="settings-row__value settings-row__value--plain">Заглушка для разработки</span>
            ) : (
              <ModelSelect
                label="Модель для рабочих задач"
                value={view.work}
                emptyLabel="Не выбрана"
                options={(models ?? []).filter((o) => o.selfhost)}
                disabled={saving || models === null}
                onChange={(work) => chooseModels(work, view.personal)}
              />
            )}
          </div>
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Для личных задач</span>
              <span className="settings-row__hint">Можно выбрать облачную модель. Личные чаты не получают рабочих данных</span>
            </div>
            {stub ? (
              <span className="settings-row__value settings-row__value--plain">Та же, что для рабочих</span>
            ) : (
              <ModelSelect
                label="Модель для личных задач"
                value={view.personal}
                emptyLabel="Та же, что для рабочих"
                options={models ?? []}
                disabled={saving || models === null}
                onChange={(personal) => chooseModels(view.work, personal)}
              />
            )}
          </div>
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Связь с моделями</span>
              {checks.length > 0 ? (
                checks.map((h) => <RowHint key={h.text} hint={h} />)
              ) : (
                <span className="settings-row__hint">Короткий вопрос через OpenCode. Текст ответа не показывается</span>
              )}
            </div>
            <button type="button" className="settings-action" aria-label="Проверить модель для рабочих задач" onClick={() => checkModel(false)} disabled={workCheck.running || !ready || !st.modelConfigured}>
              Рабочая
            </button>
            <button type="button" className="settings-action" aria-label="Проверить модель для личных задач" onClick={() => checkModel(true)} disabled={personalCheck.running || !ready || !st.personalConfigured}>
              Личная
            </button>
          </div>
        </div>
        {!ready && !stub && <p className="settings-section__note">Список моделей появится, когда OpenCode будет на связи.</p>}
      </section>

      <section className="settings-section" aria-labelledby="settings-providers">
        <h3 className="settings-section__title" id="settings-providers">Провайдеры</h3>
        <div className="settings-group">
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Селфхост-API</span>
              <span className="settings-row__hint">{own ? (own.keySaved ? 'Свой адрес · ключ в Связке ключей' : 'Свой адрес · без ключа') : 'Модель по своему адресу, совместимая с OpenAI'}</span>
            </div>
            <span className="settings-row__value" title={m.url}>{stub ? 'Заглушка' : m.url || 'Не задан'}</span>
            <button type="button" className="settings-action" onClick={() => setDialog({ kind: 'own' })}>Изменить</button>
          </div>
          {imported.map((p) => (
            <div className="settings-row" key={p.id}>
              <div className="settings-row__label">
                <span className="settings-row__title">{p.name}</span>
                <span className="settings-row__hint">{providerHint(p)}</span>
              </div>
              <button type="button" className="settings-action" aria-label={`Ключ провайдера ${p.name}`} onClick={() => setDialog({ kind: 'key', provider: p })}>Ключ…</button>
            </div>
          ))}
          {catalog.map((p) => (
            <CatalogRow key={p.id} provider={p} onRemoved={applied} onError={setError} />
          ))}
          <div className="settings-row">
            <div className="settings-row__label">
              <span className="settings-row__title">Облачный провайдер</span>
              <span className="settings-row__hint">OpenAI, Anthropic, OpenRouter и другие из каталога OpenCode. Только для личных задач</span>
            </div>
            <button type="button" className="settings-action" onClick={() => setDialog({ kind: 'connect' })} disabled={!ready}>
              Подключить…
            </button>
          </div>
        </div>
        <p className="settings-section__note">
          {view.userConfig
            ? `Из настроек OpenCode (${view.userConfig}) перенесены только провайдеры и модели: без ключей, MCP-серверов, плагинов и прав.`
            : 'Настройки OpenCode в ~/.config/opencode не найдены. Провайдеров можно подключить здесь.'}
          {view.userSkipped > 0 && ` Не перенесено провайдеров: ${view.userSkipped} — приложение понимает API, совместимые с OpenAI.`}
        </p>
      </section>

      {view.note && <p className="settings-note settings-note--warning">{view.note}</p>}
      {error && <p className="settings-note settings-row__hint--error" role="alert">{error}</p>}
      <p className="settings-note">
        У первой и второй версии OpenCode разный формат настроек. Приложение работает с одной закреплённой версией и при запуске проверяет, что установлена именно она.
      </p>

      {dialog?.kind === 'own' && <ModelDialog view={view} onClose={() => setDialog(null)} onSaved={applied} />}
      {dialog?.kind === 'key' && <KeyDialog provider={dialog.provider} onClose={() => setDialog(null)} onSaved={applied} />}
      {dialog?.kind === 'connect' && <ConnectDialog onClose={() => setDialog(null)} onSaved={applied} />}
    </>
  );
}

function RowHint({ hint }: { hint: Hint }) {
  return (
    <span className={`settings-row__hint${hint.tone ? ` settings-row__hint--${hint.tone}` : ''}`} aria-live="polite">
      {hint.text}
    </span>
  );
}

function modelCount(n: number): string {
  const tail = n % 10 === 1 && n % 100 !== 11 ? 'модель' : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20) ? 'модели' : 'моделей';
  return `${n} ${tail}`;
}

function providerHint(p: ProviderView): string {
  const kind = p.selfhost ? 'Селфхост' : 'Облако';
  let key = 'без ключа';
  if (p.keySaved) key = 'ключ в Связке ключей';
  else if (p.dropped > 0) key = 'ключ из настроек OpenCode не перенесён — задайте его';
  return `${kind} · ${modelCount(p.models)} · ${key}`;
}

type SelectProps = {
  label: string;
  value: string;
  emptyLabel: string;
  options: ModelOptionView[];
  disabled: boolean;
  onChange: (ref: string) => void;
};

/** Выбор модели, сгруппированный по провайдерам. */
function ModelSelect({ label, value, emptyLabel, options, disabled, onChange }: SelectProps) {
  const groups = new Map<string, ModelOptionView[]>();
  for (const o of options) groups.set(o.providerName, [...(groups.get(o.providerName) ?? []), o]);
  const known = value === '' || options.some((o) => o.ref === value);
  return (
    <span className="settings-select">
      <select aria-label={label} value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
        <option value="">{emptyLabel}</option>
        {!known && <option value={value}>{value}</option>}
        {[...groups.entries()].map(([provider, list]) => (
          <optgroup key={provider} label={provider}>
            {list.map((o) => <option key={o.ref} value={o.ref}>{o.name}</option>)}
          </optgroup>
        ))}
      </select>
      <UpDownIcon size="var(--icon-xs)" />
    </span>
  );
}

type CatalogRowProps = {
  provider: ProviderView;
  onRemoved: (view: AgentSettingsView) => void;
  onError: (error: string) => void;
};

/** Подключённый облачный провайдер. Отключение — вторым нажатием. */
function CatalogRow({ provider: p, onRemoved, onError }: CatalogRowProps) {
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const remove = () => {
    if (!confirm) {
      setConfirm(true);
      return;
    }
    setBusy(true);
    SettingsService.RemoveProvider(p.id)
      .then(onRemoved)
      .catch((e) => onError(errorText(e)))
      .finally(() => setBusy(false));
  };
  return (
    <div className="settings-row">
      <div className="settings-row__label">
        <span className="settings-row__title">{p.name}</span>
        <span className="settings-row__hint">Облако · {p.keySaved ? 'ключ в Связке ключей' : 'без ключа'}</span>
      </div>
      <button type="button" className="settings-action settings-action--danger" aria-label={confirm ? `Точно отключить ${p.name}?` : `Отключить ${p.name}`} onClick={remove} disabled={busy} onBlur={() => setConfirm(false)}>
        {confirm ? 'Точно отключить?' : 'Отключить'}
      </button>
    </div>
  );
}

type KeyDialogProps = {
  provider: ProviderView;
  onClose: () => void;
  onSaved: (view: AgentSettingsView) => void;
};

/** Ключ провайдера из настроек OpenCode. Уходит в Связку ключей. */
function KeyDialog({ provider: p, onClose, onSaved }: KeyDialogProps) {
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = async (action: () => Promise<AgentSettingsView>) => {
    setBusy(true);
    setError(null);
    try {
      const v = await action();
      setKey('');
      onSaved(v);
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  return (
    <Modal label={`Ключ: ${p.name}`} onClose={onClose} dismissOnOverlay={key === ''}>
      <form
        className="card"
        onSubmit={(e) => {
          e.preventDefault();
          run(() => SettingsService.SaveProviderKey({ id: p.id, key }));
        }}
      >
        <div className="card__header">
          <h2 className="card__heading model-form__title">Ключ: {p.name}</h2>
          <button type="button" className="icon-button card__close" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon />
          </button>
        </div>
        <Field label="Ключ API" htmlFor="provider-key">
          <input
            id="provider-key"
            className="text-input"
            type="password"
            placeholder={p.keySaved ? 'Сохранён. Введите новый, чтобы заменить' : 'Ключ провайдера'}
            value={key}
            autoFocus
            autoComplete="off"
            spellCheck={false}
            onChange={(e) => { setKey(e.target.value); setError(null); }}
          />
          <p className="field__hint">Ключ хранится в Связке ключей macOS. OpenCode получает его через переменную окружения, в файлы настроек он не попадает.</p>
        </Field>
        {error && <p className="field__error" role="alert">{error}</p>}
        <div className="card__footer">
          {p.keySaved ? (
            <button type="button" className="danger-button" disabled={busy} onClick={() => run(() => SettingsService.RemoveProvider(p.id))}>
              Удалить ключ
            </button>
          ) : <span />}
          <div className="model-form__actions">
            <button type="button" className="secondary-button" onClick={onClose} disabled={busy}>Отмена</button>
            <button type="submit" className="primary-button" disabled={busy || key.trim() === ''}>{busy ? 'Сохраняю…' : 'Сохранить'}</button>
          </div>
        </div>
      </form>
    </Modal>
  );
}

type ConnectDialogProps = {
  onClose: () => void;
  onSaved: (view: AgentSettingsView) => void;
};

/** Подключение облачного провайдера из каталога OpenCode по ключу. */
function ConnectDialog({ onClose, onSaved }: ConnectDialogProps) {
  const [options, setOptions] = useState<CatalogOptionView[] | null>(null);
  const [id, setId] = useState('');
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    SettingsService.CatalogProviders()
      .then((list) => setOptions(list ?? []))
      .catch((e) => {
        setOptions([]);
        setError(errorText(e));
      });
  }, []);

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      const v = await SettingsService.SaveProviderKey({ id, key });
      setKey('');
      onSaved(v);
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  return (
    <Modal label="Облачный провайдер" onClose={onClose} dismissOnOverlay={key === ''}>
      <form
        className="card"
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <div className="card__header">
          <h2 className="card__heading model-form__title">Облачный провайдер</h2>
          <button type="button" className="icon-button card__close" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon />
          </button>
        </div>
        <Field label="Провайдер" htmlFor="catalog-provider">
          <select
            id="catalog-provider"
            className="text-input"
            value={id}
            disabled={options === null}
            onChange={(e) => { setId(e.target.value); setError(null); }}
          >
            <option value="">{options === null ? 'Загружаю каталог…' : 'Выберите провайдера'}</option>
            {(options ?? []).map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
          </select>
          <p className="field__hint">Модели облачного провайдера можно выбрать только для личных задач.</p>
        </Field>
        <Field label="Ключ API" htmlFor="catalog-key">
          <input
            id="catalog-key"
            className="text-input"
            type="password"
            placeholder="Ключ провайдера"
            value={key}
            autoComplete="off"
            spellCheck={false}
            onChange={(e) => { setKey(e.target.value); setError(null); }}
          />
          <p className="field__hint">Ключ хранится в Связке ключей macOS. OpenCode получает его через переменную окружения.</p>
        </Field>
        {error && <p className="field__error" role="alert">{error}</p>}
        <div className="card__footer">
          <p className="field__hint">После подключения OpenCode перезапустится.</p>
          <div className="model-form__actions">
            <button type="button" className="secondary-button" onClick={onClose} disabled={busy}>Отмена</button>
            <button type="submit" className="primary-button" disabled={busy || id === '' || key.trim() === ''}>{busy ? 'Подключаю…' : 'Подключить'}</button>
          </div>
        </div>
      </form>
    </Modal>
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
