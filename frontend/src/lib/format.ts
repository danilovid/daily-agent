// Форматирование дат и чисел по-русски.

const MONTHS_GENITIVE = [
  'января', 'февраля', 'марта', 'апреля', 'мая', 'июня',
  'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря',
];
const MONTHS_SHORT = ['янв', 'фев', 'мар', 'апр', 'мая', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'];
export const MONTHS_NOMINATIVE = [
  'Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь',
  'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь',
];
const WEEKDAYS = ['воскресенье', 'понедельник', 'вторник', 'среда', 'четверг', 'пятница', 'суббота'];
export const WEEKDAYS_SHORT = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс'];

/** Дата YYYY-MM-DD как локальная полночь. */
export function parseDay(day: string): Date {
  const [y, m, d] = day.split('-').map(Number);
  return new Date(y, m - 1, d);
}

/** Локальная дата в формате YYYY-MM-DD. */
export function toDay(date: Date): string {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, '0');
  const d = String(date.getDate()).padStart(2, '0');
  return `${y}-${m}-${d}`;
}

function addDays(day: string, n: number): string {
  const d = parseDay(day);
  d.setDate(d.getDate() + n);
  return toDay(d);
}

/** «вторник, 24 ноября» */
export function longDate(day: string): string {
  const d = parseDay(day);
  return `${WEEKDAYS[d.getDay()]}, ${d.getDate()} ${MONTHS_GENITIVE[d.getMonth()]}`;
}

/** «24 ноября» */
export function dayMonth(date: Date): string {
  return `${date.getDate()} ${MONTHS_GENITIVE[date.getMonth()]}`;
}

/** «24 ноя» */
export function shortDate(day: string): string {
  const d = parseDay(day);
  return `${d.getDate()} ${MONTHS_SHORT[d.getMonth()]}`;
}

/** «10:00» */
export function timeOfDay(iso: string): string {
  const d = new Date(iso);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/** Форма слова для числа: plural(2, ['встреча', 'встречи', 'встреч']) → «встречи». */
export function plural(n: number, forms: [string, string, string]): string {
  const mod10 = n % 10;
  const mod100 = n % 100;
  if (mod10 === 1 && mod100 !== 11) return forms[0];
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return forms[1];
  return forms[2];
}

/** «2 встречи · 3 задачи» */
export function agendaLine(meetings: number, tasks: number): string {
  return `${meetings} ${plural(meetings, ['встреча', 'встречи', 'встреч'])} · ${tasks} ${plural(tasks, ['задача', 'задачи', 'задач'])}`;
}

/** «сегодня, 08:15», «вчера, 18:40» или «22 ноя, 11:00» относительно дня today. */
export function createdLabel(iso: string, today: string): string {
  const d = new Date(iso);
  const day = toDay(d);
  const time = timeOfDay(iso);
  if (day === today) return `сегодня, ${time}`;
  if (day === addDays(today, -1)) return `вчера, ${time}`;
  return `${shortDate(day)}, ${time}`;
}

/** Метка срока задачи относительно дня today или пустая строка. */
export function dueLabel(due: string, today: string): { text: string; overdue: boolean } | null {
  if (!due) return null;
  if (due < today) return { text: 'просрочено', overdue: true };
  if (due === today) return { text: 'срок сегодня', overdue: false };
  if (due === addDays(today, 1)) return { text: 'срок завтра', overdue: false };
  return { text: `срок ${shortDate(due)}`, overdue: false };
}

/** «через 42 мин», «через 1 ч 20 мин», «идёт» или null, если встреча закончилась. */
export function meetingCountdown(startsAt: string, durationMin: number, now: Date): string | null {
  const start = new Date(startsAt).getTime();
  const end = start + durationMin * 60_000;
  const t = now.getTime();
  if (t >= end) return null;
  if (t >= start) return 'идёт';
  const minutes = Math.ceil((start - t) / 60_000);
  if (minutes < 60) return `через ${minutes} мин`;
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m === 0 ? `через ${h} ч` : `через ${h} ч ${m} мин`;
}
