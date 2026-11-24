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
// Короткие названия дней недели в порядке Date.getDay().
const WEEKDAYS_SHORT_BY_DAY = ['Вс', 'Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб'];

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/** Заголовок дня в списке встреч: «Сегодня, 24 ноября», «Завтра, 25 ноября», «Четверг, 26 ноября». */
export function dayHeading(day: string, today: string): string {
  const d = parseDay(day);
  const date = `${d.getDate()} ${MONTHS_GENITIVE[d.getMonth()]}`;
  if (day === today) return `Сегодня, ${date}`;
  if (day === addDays(today, 1)) return `Завтра, ${date}`;
  if (day === addDays(today, -1)) return `Вчера, ${date}`;
  return `${capitalize(WEEKDAYS[d.getDay()])}, ${date}`;
}

/** Дата в поле карточки: «Вт, 24 ноября». */
export function pickerDate(day: string): string {
  const d = parseDay(day);
  return `${WEEKDAYS_SHORT_BY_DAY[d.getDay()]}, ${d.getDate()} ${MONTHS_GENITIVE[d.getMonth()]}`;
}

/** Срок в поле карточки задачи: «Сегодня», «Завтра» или «Вт, 24 ноября». */
export function dueDateLabel(day: string, today: string): string {
  if (!day) return 'Без срока';
  if (day === today) return 'Сегодня';
  if (day === addDays(today, 1)) return 'Завтра';
  if (day === addDays(today, -1)) return 'Вчера';
  return pickerDate(day);
}

/** «30 мин», «60 мин», «90 мин», «2 ч», «2 ч 30 мин» — как в макетах. */
export function durationLabel(minutes: number): string {
  if (minutes < 120) return `${minutes} мин`;
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m === 0 ? `${h} ч` : `${h} ч ${m} мин`;
}

/** «4 предстоящие», «1 предстоящая», «5 предстоящих». */
export function upcomingLabel(n: number): string {
  return `${n} ${plural(n, ['предстоящая', 'предстоящие', 'предстоящих'])}`;
}

/** Время «ЧЧ:ММ» из Date. */
export function hhmm(date: Date): string {
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
}

/** Ближайшие полчаса после now: время начала новой встречи по умолчанию. */
export function nextHalfHour(now: Date): Date {
  const d = new Date(now);
  d.setSeconds(0, 0);
  d.setMinutes(d.getMinutes() < 30 ? 30 : 60);
  return d;
}

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

export function addDays(day: string, n: number): string {
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

/** Подпись отсчёта для строки встречи: только у сегодняшних встреч, иначе null. */
export function countdownLabel(startsAt: string, durationMin: number, now: Date): string | null {
  if (toDay(new Date(startsAt)) !== toDay(now)) return null;
  return meetingCountdown(startsAt, durationMin, now);
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
