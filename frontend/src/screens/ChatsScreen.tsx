import { useCallback, useEffect, useRef, useState } from 'react';
import { ChatsService } from '../../bindings/daily-agent/internal/app';
import type { ChatListItem, ChatView } from '../../bindings/daily-agent/internal/app';
import { Conversation } from '../components/chat/Conversation';
import { MoreIcon, PlusIcon } from '../components/Icons';
import { useData, useLoad } from '../lib/data';
import { addDays, dayHeading, shortDate, timeOfDay, toDay } from '../lib/format';

type Props = {
  selectedId: string | null;
  onSelect: (id: string | null) => void;
};

type Group = { title: string; items: ChatListItem[] };

function groupChats(chats: ChatListItem[], today: string): Group[] {
  const yesterday = addDays(today, -1);
  const groups: Group[] = [
    { title: 'Сегодня', items: [] },
    { title: 'Вчера', items: [] },
    { title: 'Ранее', items: [] },
  ];
  for (const c of chats) {
    const day = toDay(new Date(c.updatedAt));
    groups[day === today ? 0 : day === yesterday ? 1 : 2].items.push(c);
  }
  return groups.filter((g) => g.items.length > 0);
}

function itemTime(c: ChatListItem, today: string): string {
  const day = toDay(new Date(c.updatedAt));
  return day === today || day === addDays(today, -1) ? timeOfDay(c.updatedAt) : shortDate(day);
}

/** Раздел «Чаты»: список переписок и выбранная переписка. */
export function ChatsScreen({ selectedId, onSelect }: Props) {
  const { refresh, showError } = useData();
  const { data } = useLoad(() => ChatsService.List(), []);
  const [view, setView] = useState<ChatView | null>(null);
  const today = toDay(new Date());
  const chats = data ?? [];

  useEffect(() => {
    if (!selectedId) setView(null);
  }, [selectedId]);

  const create = useCallback(async () => {
    const v = await ChatsService.Create(false);
    onSelect(v.id);
    refresh();
    return v.id;
  }, [onSelect, refresh]);

  const onView = useCallback((v: ChatView) => setView(v), []);
  const title = view?.title ?? 'Новый чат';
  const meta = view
    ? [
      `${dayHeading(toDay(new Date(view.createdAt)), today).split(',')[0]}, ${timeOfDay(view.createdAt)}`,
      view.context ? `контекст: ${view.context}` : '',
    ].filter(Boolean).join(' · ')
    : 'Задайте вопрос, и чат появится в списке';

  return (
    <>
      <section className="chat-list" aria-label="Список чатов">
        <div className="chat-list__header drag-region">
          <h2 className="screen-title">Чаты</h2>
          <button className="icon-button" aria-label="Новый чат" onClick={() => onSelect(null)}>
            <PlusIcon size="var(--icon-nav)" />
          </button>
        </div>
        <div className="chat-list__items">
          {data && chats.length === 0 && <p className="chat-list__empty">Чатов пока нет.</p>}
          {groupChats(chats, today).map((g) => (
            <div key={g.title} className="chat-list__group">
              <h3 className="chat-list__group-title">{g.title}</h3>
              {g.items.map((c) => (
                <button
                  key={c.id}
                  className={`chat-item${c.id === selectedId ? ' chat-item--active' : ''}`}
                  aria-current={c.id === selectedId ? 'true' : undefined}
                  onClick={() => onSelect(c.id)}
                >
                  <span className="chat-item__top">
                    <span className="chat-item__title">{c.title}</span>
                    <span className="chat-item__time">{c.running ? 'отвечает…' : itemTime(c, today)}</span>
                  </span>
                  {c.preview && <span className="chat-item__preview">{c.preview}</span>}
                </button>
              ))}
            </div>
          ))}
        </div>
      </section>

      <section className="chat-view" aria-label="Переписка">
        <div className="chat-view__header drag-region">
          <div className="chat-view__heading">
            <h3 className="chat-view__title">{title}</h3>
            <p className="chat-view__meta">{meta}</p>
          </div>
          {view && (
            <ChatMenu
              key={view.id}
              title={view.title}
              onRename={(t) => ChatsService.Rename(view.id, t).then(() => {
                setView({ ...view, title: t });
                refresh();
              }).catch(showError)}
              onDelete={() => ChatsService.Delete(view.id).then(() => {
                onSelect(null);
                refresh();
              }).catch(showError)}
            />
          )}
        </div>
        <Conversation
          chatId={selectedId}
          variant="screen"
          onCreate={create}
          onView={onView}
          onDone={refresh}
          emptyText="Спросите агента о чём угодно. Переписка сохранится в этом списке."
        />
      </section>
    </>
  );
}

type MenuProps = { title: string; onRename: (title: string) => void; onDelete: () => void };

/** Меню «Действия с чатом»: переименовать и удалить. */
function ChatMenu({ title, onRename, onDelete }: MenuProps) {
  const [open, setOpen] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [value, setValue] = useState(title);
  const root = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) {
        setOpen(false);
        setRenaming(false);
        setConfirm(false);
      }
    };
    window.addEventListener('mousedown', close);
    return () => window.removeEventListener('mousedown', close);
  }, [open]);

  return (
    <div className="chat-menu" ref={root}>
      <button className="icon-button" aria-label="Действия с чатом" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        <MoreIcon />
      </button>
      {open && (
        <div className="chat-menu__popover" role="menu">
          {renaming ? (
            <form
              className="chat-menu__rename"
              onSubmit={(e) => {
                e.preventDefault();
                onRename(value);
                setOpen(false);
                setRenaming(false);
              }}
            >
              <label htmlFor="chat-rename" className="visually-hidden">Название чата</label>
              <input
                id="chat-rename"
                className="text-input"
                value={value}
                maxLength={80}
                autoFocus
                onChange={(e) => setValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') {
                    e.stopPropagation();
                    setRenaming(false);
                  }
                }}
              />
            </form>
          ) : (
            <>
              <button role="menuitem" className="chat-menu__item" onClick={() => { setValue(title); setRenaming(true); }}>
                Переименовать
              </button>
              <button
                role="menuitem"
                className="chat-menu__item chat-menu__item--danger"
                onClick={() => (confirm ? onDelete() : setConfirm(true))}
              >
                {confirm ? 'Точно удалить?' : 'Удалить чат'}
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
