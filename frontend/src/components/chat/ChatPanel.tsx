import { useCallback, useState } from 'react';
import { ChatsService } from '../../../bindings/daily-agent/internal/app';
import type { ChatView } from '../../../bindings/daily-agent/internal/app';
import { useData } from '../../lib/data';
import { dayMonth } from '../../lib/format';
import { ChatIcon, CloseIcon, ExpandIcon, PlusIcon } from '../Icons';
import { Conversation } from './Conversation';

type PanelProps = {
  chatId: string | null;
  onChatChange: (id: string | null) => void;
  onOpenBig: (id: string | null) => void;
};

/** Маленький чат поверх экрана. Он рабочий: новый чат получает агенду на сегодня. */
export function ChatPanel({ chatId, onChatChange, onOpenBig }: PanelProps) {
  const { refresh } = useData();
  const [context, setContext] = useState('');

  const create = useCallback(async () => {
    const view = await ChatsService.Create(true, false);
    setContext(view.context);
    onChatChange(view.id);
    return view.id;
  }, [onChatChange]);

  const onView = useCallback((v: ChatView) => setContext(v.context), []);
  // Пока чата нет, контекст будет агендой на сегодня.
  const contextLine = chatId ? (context ? `Контекст: ${context}` : 'Без контекста') : `Контекст: агенда на ${dayMonth(new Date())}`;

  return (
    <section className="chat-panel" aria-label="Чат с агентом">
      <div className="chat-panel__header">
        <div className="chat-panel__heading">
          <h2 className="chat-panel__title">Агент</h2>
          <p className="chat-panel__context">{contextLine}</p>
        </div>
        <div className="chat-panel__actions">
          <button className="icon-button" aria-label="Открыть большой чат" onClick={() => onOpenBig(chatId)}>
            <ExpandIcon />
          </button>
          <button
            className="icon-button"
            aria-label="Новый чат"
            onClick={() => {
              setContext('');
              onChatChange(null);
            }}
          >
            <PlusIcon size="var(--icon-nav)" />
          </button>
        </div>
      </div>
      <Conversation
        chatId={chatId}
        variant="panel"
        onCreate={create}
        onView={onView}
        onDone={refresh}
        emptyText="Спросите о сегодняшнем дне: агент видит встречи и задачи из агенды."
      />
    </section>
  );
}

/** Круглая кнопка чата в углу окна. */
export function ChatFab({ open, onToggle }: { open: boolean; onToggle: () => void }) {
  return (
    <button
      className={`chat-fab${open ? ' chat-fab--open' : ''}`}
      aria-label={open ? 'Закрыть чат' : 'Открыть чат с агентом'}
      aria-expanded={open}
      onClick={onToggle}
    >
      {open ? <CloseIcon size="var(--icon-fab)" /> : <ChatIcon size="var(--icon-fab)" />}
    </button>
  );
}
