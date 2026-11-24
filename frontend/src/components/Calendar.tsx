import { ChevronLeftIcon, ChevronRightIcon } from './Icons';
import { MONTHS_NOMINATIVE, WEEKDAYS_SHORT, dayMonth, parseDay } from '../lib/format';

type Props = {
  /** Месяц в формате YYYY-MM. */
  month: string;
  /** Сегодняшний день в формате YYYY-MM-DD. */
  today: string;
  /** Дни месяца, в которые есть встречи. */
  meetingDays: number[];
  onMonthChange: (month: string) => void;
};

function shiftMonth(month: string, delta: number): string {
  const [y, m] = month.split('-').map(Number);
  const d = new Date(y, m - 1 + delta, 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
}

export function Calendar({ month, today, meetingDays, onMonthChange }: Props) {
  const [year, monthNum] = month.split('-').map(Number);
  const first = new Date(year, monthNum - 1, 1);
  const daysInMonth = new Date(year, monthNum, 0).getDate();
  // Неделя начинается с понедельника.
  const leading = (first.getDay() + 6) % 7;
  const todayDate = parseDay(today);
  const isTodayMonth = todayDate.getFullYear() === year && todayDate.getMonth() === monthNum - 1;
  const withMeetings = new Set(meetingDays);

  const cells: (number | null)[] = [
    ...Array.from({ length: leading }, () => null),
    ...Array.from({ length: daysInMonth }, (_, i) => i + 1),
  ];

  return (
    <div className="calendar">
      <div className="calendar__header">
        <h2 className="calendar__title">
          {MONTHS_NOMINATIVE[monthNum - 1]} <span className="calendar__year">{year}</span>
        </h2>
        <div className="calendar__nav">
          <button className="icon-button" aria-label="Предыдущий месяц" onClick={() => onMonthChange(shiftMonth(month, -1))}>
            <ChevronLeftIcon />
          </button>
          <button className="icon-button" aria-label="Следующий месяц" onClick={() => onMonthChange(shiftMonth(month, 1))}>
            <ChevronRightIcon />
          </button>
        </div>
      </div>
      <div className="calendar__weekdays">
        {WEEKDAYS_SHORT.map((w) => <span key={w}>{w}</span>)}
      </div>
      <div className="calendar__grid">
        {cells.map((day, i) => {
          if (day === null) return <span key={`blank-${i}`} />;
          const isToday = isTodayMonth && day === todayDate.getDate();
          const hasMeetings = withMeetings.has(day);
          const label = [
            dayMonth(new Date(year, monthNum - 1, day)),
            isToday ? 'сегодня' : '',
            hasMeetings ? 'есть встречи' : '',
          ].filter(Boolean).join(', ');
          return (
            <button
              key={day}
              className={`calendar__day${isToday ? ' calendar__day--today' : ''}`}
              aria-current={isToday ? 'date' : undefined}
              aria-label={label}
            >
              {isToday ? <span className="calendar__today-mark">{day}</span> : day}
              {hasMeetings && !isToday && <span className="calendar__dot" aria-hidden="true" />}
            </button>
          );
        })}
      </div>
    </div>
  );
}
