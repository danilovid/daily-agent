// Разделы приложения.

export type Section = 'today' | 'inbox' | 'backlog' | 'meetings' | 'chats' | 'settings';

export const SECTION_TITLES: Record<Section, string> = {
  today: 'Сегодня',
  inbox: 'Входящие',
  backlog: 'Бэклог',
  meetings: 'Встречи',
  chats: 'Чаты',
  settings: 'Настройки',
};
