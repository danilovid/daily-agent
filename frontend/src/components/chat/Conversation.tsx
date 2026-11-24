import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { ChatsService } from '../../../bindings/daily-agent/internal/app';
import type { ChatEvent, ChatMessageView, ChatView } from '../../../bindings/daily-agent/internal/app';
import { agentSettingsHint, agentUnavailable, useAgentStatus } from '../../lib/agent';
import { useCards } from '../../lib/cards';
import { errorText } from '../../lib/data';
import { NoteIcon, SendIcon, StopIcon, ToolIcon } from '../Icons';

type Props = {
  chatId: string | null;
  /** panel — маленький чат поверх экрана, screen — раздел «Чаты». */
  variant: 'panel' | 'screen';
  /** Создать чат перед первым сообщением. Возвращает его идентификатор. */
  onCreate: () => Promise<string>;
  /** Чат загружен или изменился: заголовок, контекст. */
  onView?: (view: ChatView) => void;
  /** Агент закончил отвечать: можно обновить список чатов. */
  onDone?: () => void;
  /** Текст, когда переписки ещё нет. */
  emptyText: string;
  /** Личный чат: своя модель, без рабочих данных. */
  personal?: boolean;
};

/** Переписка с агентом: история, ответ по мере готовности, поле ввода. */
export function Conversation({ chatId, variant, onCreate, onView, onDone, emptyText, personal = false }: Props) {
  const status = useAgentStatus();
  const blocked = agentUnavailable(status, personal);
  const settingsHint = agentSettingsHint(status, personal);
  const { openSettings } = useCards();
  const [view, setView] = useState<ChatView | null>(null);
  const [pending, setPending] = useState<ChatMessageView[]>([]);
  const [streaming, setStreaming] = useState('');
  const [running, setRunning] = useState(false);
  const [input, setInput] = useState('');
  const [error, setError] = useState<string | null>(null);
  const currentId = useRef<string | null>(chatId);
  // Чат создаётся первым сообщением: смена chatId в этот момент не сбрасывает переписку.
  const creating = useRef(false);
  const list = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLInputElement>(null);

  const load = useCallback(async (id: string) => {
    const v = await ChatsService.Open(id);
    if (currentId.current !== id) return;
    setView(v);
    setRunning(v.running);
    setPending([]);
    setStreaming('');
    onView?.(v);
  }, [onView]);

  // Смена чата: загрузить переписку.
  useEffect(() => {
    if (creating.current && chatId) {
      currentId.current = chatId;
      return;
    }
    currentId.current = chatId;
    setView(null);
    setPending([]);
    setStreaming('');
    setRunning(false);
    setError(null);
    if (chatId) load(chatId).catch((e) => setError(errorText(e)));
  }, [chatId, load]);

  // Ход ответа агента.
  useEffect(() => {
    const off = Events.On('chat:event', (event) => {
      const e = event.data as ChatEvent;
      if (!e || e.chatId !== currentId.current) return;
      if (e.type === 'started') setRunning(true);
      if (e.type === 'delta') setStreaming((s) => s + e.delta);
      if (e.type === 'done') {
        setRunning(false);
        load(e.chatId).catch((err) => setError(errorText(err)));
        onDone?.();
      }
    });
    return () => off();
  }, [load, onDone]);

  // Маленький чат ставит курсор в поле, как только агенту можно писать:
  // при открытии поле ещё заблокировано, пока не пришло состояние агента.
  useEffect(() => {
    if (variant === 'panel' && !blocked) field.current?.focus();
  }, [variant, blocked]);

  // Новые строки — прокрутка вниз.
  useLayoutEffect(() => {
    const el = list.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [view, pending, streaming, running]);

  const send = async () => {
    const text = input.trim();
    if (!text || running || blocked) return;
    setError(null);
    setInput('');
    setPending([{ id: 'pending', role: 'user', text, createdAt: '' }]);
    setRunning(true);
    try {
      let id = currentId.current;
      if (!id) {
        creating.current = true;
        id = await onCreate();
        currentId.current = id;
      }
      await ChatsService.Send(id, text);
      creating.current = false;
    } catch (e) {
      creating.current = false;
      setPending([]);
      setRunning(false);
      setInput(text);
      setError(errorText(e));
    }
  };

  const stop = () => {
    const id = currentId.current;
    if (id) ChatsService.Stop(id).catch((e) => setError(errorText(e)));
  };

  const messages = [...(view?.messages ?? []), ...pending];
  const unavailable = blocked ?? (view?.unavailable || null);
  const placeholder = blocked ?? 'Спросить агента…';

  return (
    <div className={`conversation conversation--${variant}`}>
      <div className="conversation__messages" ref={list} aria-live="polite">
        {messages.length === 0 && !running && !settingsHint && <p className="conversation__empty">{emptyText}</p>}
        {messages.map((m) => <MessageRow key={m.id} message={m} />)}
        {streaming && <p className="msg-agent">{streaming}</p>}
        {running && !streaming && <p className="msg-note msg-note--pending">Агент думает…</p>}
        {settingsHint ? (
          <div className={`conversation__notice${messages.length === 0 ? ' conversation__notice--alone' : ''}`}>
            <p className="conversation__notice-text">{settingsHint}</p>
            <button type="button" className="text-button text-button--accent" onClick={openSettings}>
              Открыть настройки
            </button>
          </div>
        ) : (
          unavailable && view && <p className="msg-note">{unavailable}</p>
        )}
        {error && <p className="msg-note msg-note--error" role="alert">{error}</p>}
      </div>
      <form
        className="conversation__input"
        onSubmit={(e) => {
          e.preventDefault();
          send();
        }}
      >
        <div className="chat-input">
          <label htmlFor={`chat-input-${variant}`} className="visually-hidden">Сообщение агенту</label>
          <input
            ref={field}
            id={`chat-input-${variant}`}
            type="text"
            placeholder={placeholder}
            value={input}
            disabled={!!blocked}
            autoComplete="off"
            onChange={(e) => setInput(e.target.value)}
          />
          {running ? (
            <button type="button" className="chat-send chat-send--stop" aria-label="Остановить ответ" onClick={stop}>
              <StopIcon />
            </button>
          ) : (
            <button type="submit" className="chat-send" aria-label="Отправить" disabled={!input.trim() || !!blocked}>
              <SendIcon />
            </button>
          )}
        </div>
      </form>
    </div>
  );
}

function MessageRow({ message: m }: { message: ChatMessageView }) {
  switch (m.role) {
    case 'user':
      return <p className="msg-user">{m.text}</p>;
    case 'agent':
      return <p className="msg-agent">{m.text}</p>;
    case 'tool':
      return <p className="msg-note"><ToolIcon />{m.text}</p>;
    case 'context':
      return <p className="msg-note"><NoteIcon />{m.text}</p>;
    case 'error':
      return <p className="msg-note msg-note--error"><NoteIcon />{m.text}</p>;
    default:
      return null;
  }
}
