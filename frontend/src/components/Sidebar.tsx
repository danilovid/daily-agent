import type { ReactNode } from 'react';
import type { SidebarView } from '../../bindings/daily-agent/internal/app/models';
import { SECTION_TITLES, type Section } from '../sections';
import { agendaLine, shortDate } from '../lib/format';
import { Calendar } from './Calendar';
import { TagChip } from './TagChip';
import {
  CalendarIcon, ChatIcon, ClockIcon, InboxIcon, ListIcon, PlusIcon, SearchIcon, SettingsIcon,
} from './Icons';

type Props = {
  data: SidebarView | null;
  section: Section;
  onSelect: (section: Section) => void;
  onMonthChange: (month: string) => void;
};

type NavItem = {
  section: Section;
  icon: ReactNode;
  trailing: ReactNode;
};

export function Sidebar({ data, section, onSelect, onMonthChange }: Props) {
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
            <input id="search-input" type="text" placeholder="Поиск по задачам и встречам" autoComplete="off" spellCheck={false} />
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
              <button key={tag.id} className="tag-button">
                <TagChip tag={tag} variant="sidebar" />
              </button>
            ))}
            <button className="icon-button sidebar__new-tag" aria-label="Новый тег">
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
              onMonthChange={onMonthChange}
            />
          </div>
        )}
      </div>

      <div className="sidebar__footer">
        <p className="agent-status">
          <span className="agent-status__dot" aria-hidden="true" />
          Агент не подключён
        </p>
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
