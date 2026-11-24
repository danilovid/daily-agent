import { useCallback, useEffect, useMemo, useState } from 'react';
import type { TagView } from '../bindings/daily-agent/internal/app';
import { ChatFab, ChatPanel } from './components/chat/ChatPanel';
import { MeetingCard } from './components/MeetingCard';
import { Modal } from './components/Modal';
import { Sidebar } from './components/Sidebar';
import { NewTagForm } from './components/TagEditor';
import { TaskCard } from './components/TaskCard';
import { CardsContext, type OpenCard } from './lib/cards';
import { DataProvider, useData } from './lib/data';
import { ChatsScreen } from './screens/ChatsScreen';
import { MeetingsScreen } from './screens/MeetingsScreen';
import { SettingsScreen } from './screens/SettingsScreen';
import { SearchScreen } from './screens/SearchScreen';
import { TasksScreen } from './screens/TasksScreen';
import { TodayScreen } from './screens/TodayScreen';
import type { Section } from './sections';

export default function App() {
  return (
    <DataProvider>
      <Shell />
    </DataProvider>
  );
}

function Shell() {
  const { error, refresh } = useData();
  const [section, setSection] = useState<Section>('today');
  const [tagFilter, setTagFilter] = useState<TagView | null>(null);
  const [query, setQuery] = useState('');
  const [searchQuery, setSearchQuery] = useState('');
  const [card, setCard] = useState<OpenCard | null>(null);
  // Маленький чат: открыт ли и какой в нём чат. Большой чат: выбранный чат.
  const [chatOpen, setChatOpen] = useState(false);
  const [panelChat, setPanelChat] = useState<string | null>(null);
  const [bigChat, setBigChat] = useState<string | null>(null);

  // Поиск запускается, когда ввод замирает на 200 мс.
  useEffect(() => {
    const id = window.setTimeout(() => setSearchQuery(query.trim()), 200);
    return () => window.clearTimeout(id);
  }, [query]);
  const searching = searchQuery !== '';

  const select = useCallback((s: Section) => {
    setQuery('');
    setSearchQuery('');
    setTagFilter(null);
    setSection(s);
  }, []);

  const cards = useMemo(() => ({
    openTask: (id?: string) => setCard({ kind: 'task', id }),
    openMeeting: (id?: string) => setCard({ kind: 'meeting', id }),
    openSettings: () => {
      setChatOpen(false);
      select('settings');
    },
    openChat: (id: string) => {
      setPanelChat(id);
      setChatOpen(true);
    },
  }), [select]);
  const closeCard = useCallback(() => setCard(null), []);

  const toggleTag = (tag: TagView) => {
    setQuery('');
    setSearchQuery('');
    if (tagFilter?.id === tag.id) {
      setTagFilter(null);
      return;
    }
    setTagFilter(tag);
    setSection('backlog');
  };

  let screen;
  if (searching) screen = <SearchScreen query={searchQuery} />;
  else if (section === 'today') screen = <TodayScreen />;
  else if (section === 'meetings') screen = <MeetingsScreen />;
  else if (section === 'inbox') screen = <TasksScreen mode="inbox" />;
  else if (section === 'backlog') screen = <TasksScreen key={tagFilter?.id ?? 'all'} mode="backlog" tag={tagFilter} onClearTag={() => setTagFilter(null)} />;
  else if (section === 'chats') screen = <ChatsScreen selectedId={bigChat} onSelect={setBigChat} />;
  else screen = <SettingsScreen />;
  const inChats = !searching && section === 'chats';

  const openBigChat = (id: string | null) => {
    setChatOpen(false);
    setBigChat(id);
    select('chats');
  };

  return (
    <CardsContext.Provider value={cards}>
      <div className="app">
        <Sidebar
          section={searching ? null : section}
          onSelect={select}
          query={query}
          onQueryChange={setQuery}
          activeTagId={tagFilter?.id ?? null}
          onTagClick={toggleTag}
          onNewTag={() => setCard({ kind: 'new-tag' })}
        />
        <main className="main" data-screen={searching ? 'search' : section}>{screen}</main>
      </div>

      {!inChats && chatOpen && <ChatPanel chatId={panelChat} onChatChange={setPanelChat} onOpenBig={openBigChat} />}
      {!inChats && <ChatFab open={chatOpen} onToggle={() => setChatOpen((o) => !o)} />}

      {card?.kind === 'task' && <TaskCard key={card.id ?? 'new'} taskId={card.id} onClose={closeCard} />}
      {card?.kind === 'meeting' && <MeetingCard key={card.id ?? 'new'} meetingId={card.id} onClose={closeCard} />}
      {card?.kind === 'new-tag' && (
        <Modal label="Новый тег" onClose={closeCard} size="sm">
          <div className="card">
            <h2 className="card__heading">Новый тег</h2>
            <NewTagForm
              onCancel={closeCard}
              onCreated={() => {
                refresh();
                closeCard();
              }}
            />
          </div>
        </Modal>
      )}

      {error && <div className="toast" role="alert">{error}</div>}
    </CardsContext.Provider>
  );
}
