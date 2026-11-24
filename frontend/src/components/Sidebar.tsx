import { useEffect, useRef, useState, type ReactNode } from 'react';
import { SidebarService } from '../../bindings/daily-agent/internal/app';
import type { TagView } from '../../bindings/daily-agent/internal/app';
import { SECTION_TITLES, type Section } from '../sections';
import { useLoad } from '../lib/data';
import { agendaLine, shortDate } from '../lib/format';
import { AgentStatus } from './AgentStatus';
import { Calendar } from './Calendar';
import { TagChip } from './TagChip';
import {
  CalendarIcon, ChatIcon, ClockIcon, CloseIcon, InboxIcon, ListIcon, PlusIcon, SearchIcon, SettingsIcon,
} from './Icons';

type Props = {
  /** Активный раздел или null, пока открыт поиск. */
  section: Section | null;
  onSelect: (section: Section) => void;
  query: string;
  onQueryChange: (query: string) => void;
  activeTagId: string | null;
  onTagClick: (tag: TagView) => void;
  onNewTag: () => void;
};

type NavItem = {
  section: Section;
  icon: ReactNode;
  trailing: ReactNode;
};

export function Sidebar({ section, onSelect, query, onQueryChange, activeTagId, onTagClick, onNewTag }: Props) {
  // Пустая строка — текущий месяц.
  const [month, setMonth] = useState('');
  const { data } = useLoad(() => SidebarService.Sidebar(month), [month]);
  const search = useRef<HTMLInputElement>(null);

  // ⌘F переводит фокус в поиск.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey && e.key.toLowerCase() === 'f') {
        e.preventDefault();
        search.current?.focus();
        search.current?.select();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const counts = data?.counts;
  const nav: NavItem[] = [
    {
      section: 'today',
      icon: <CalendarIcon />,
      trailing: data && <span className="nav-item__date">{shortDate(data.today)}</span>,
    },
    {
      section: 'inbox',
      icon: <InboxIcon />,
      trailing: counts && counts.inbox > 0 && <span className="count-badge">{counts.inbox}</span>,
    },
    {
      section: 'backlog',
      icon: <ListIcon />,
      trailing: counts && counts.backlog > 0 && <span className="nav-item__count">{counts.backlog}</span>,
    },
    {
      section: 'meetings',
      icon: <ClockIcon />,
      trailing: counts && counts.meetingsToday > 0 && <span className="nav-item__count">{counts.meetingsToday}</span>,
    },
    {
      section: 'chats',
      icon: <ChatIcon />,
      trailing: counts && counts.chats > 0 && <span className="nav-item__count">{counts.chats}</span>,
    },
  ];

  return (
    <aside className="sidebar">
      {/* Полоса под кнопками окна: за неё окно можно перетаскивать. */}
      <div className="titlebar drag-region" />

      {/* Середина прокручивается, если окно ниже панели; подвал всегда виден. */}
      <div className="sidebar__scroll">
        <div className="sidebar__header drag-region">
          <h1 className="sidebar__title">Daily Agent</h1>
          {counts && <span className="sidebar__agenda">{agendaLine(counts.meetingsToday, counts.backlog)}</span>}
        </div>

        <div className="sidebar__search">
          <div className="search">
            <SearchIcon />
            <label htmlFor="search-input" className="visually-hidden">Поиск</label>
            <input
              id="search-input"
              ref={search}
              type="text"
              placeholder="Поиск по задачам и встречам"
              autoComplete="off"
              spellCheck={false}
              value={query}
              onChange={(e) => onQueryChange(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Escape' && query) {
                  e.stopPropagation();
                  onQueryChange('');
                }
              }}
            />
            {query && (
              <button className="search__clear" aria-label="Очистить поиск" onClick={() => onQueryChange('')}>
                <CloseIcon size="var(--icon-chevron)" />
              </button>
            )}
          </div>
        </div>

        <nav aria-label="Разделы" className="nav">
          {nav.map((item) => (
            <button
              key={item.section}
              className={`nav-item${section === item.section ? ' nav-item--active' : ''}`}
              aria-current={section === item.section ? 'page' : undefined}
              onClick={() => onSelect(item.section)}
            >
              {item.icon}
              <span>{SECTION_TITLES[item.section]}</span>
              {item.trailing}
            </button>
          ))}
        </nav>

        <div className="sidebar__tags">
          <h2 className="sidebar__subtitle">Теги</h2>
          <div className="sidebar__tag-list">
            {data?.tags?.map((tag) => (
              <button
                key={tag.id}
                className="tag-button"
                aria-pressed={activeTagId === tag.id}
                aria-label={`Задачи с тегом ${tag.name}`}
                onClick={() => onTagClick(tag)}
              >
                <TagChip tag={tag} variant="sidebar" selected={activeTagId === tag.id} />
              </button>
            ))}
            <button className="icon-button sidebar__new-tag" aria-label="Новый тег" onClick={onNewTag}>
              <PlusIcon />
            </button>
          </div>
        </div>

        {data && (
          <div className="sidebar__calendar">
            <Calendar
              month={data.month}
              today={data.today}
              meetingDays={data.meetingDays ?? []}
              onMonthChange={setMonth}
            />
          </div>
        )}
      </div>

      <div className="sidebar__footer">
        <AgentStatus />
        <button
          className={`icon-button${section === 'settings' ? ' icon-button--active' : ''}`}
          aria-label="Настройки"
          aria-current={section === 'settings' ? 'page' : undefined}
          onClick={() => onSelect('settings')}
        >
          <SettingsIcon />
        </button>
      </div>
    </aside>
  );
}
