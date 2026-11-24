import { useCallback, useEffect, useState } from 'react';
import { SidebarService, TodayService } from '../bindings/daily-agent/internal/app';
import type { SidebarView, TodayView } from '../bindings/daily-agent/internal/app';
import { Sidebar } from './components/Sidebar';
import { TodayScreen } from './screens/TodayScreen';
import { PlaceholderScreen } from './screens/PlaceholderScreen';
import { SECTION_TITLES, type Section } from './sections';

function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export default function App() {
  const [section, setSection] = useState<Section>('today');
  // Пустая строка — текущий месяц.
  const [month, setMonth] = useState('');
  const [sidebar, setSidebar] = useState<SidebarView | null>(null);
  const [today, setToday] = useState<TodayView | null>(null);
  const [error, setError] = useState<string | null>(null);

  const loadSidebar = useCallback(() => {
    SidebarService.Sidebar(month)
      .then((data) => setSidebar(data))
      .catch((e) => setError(errorText(e)));
  }, [month]);

  const loadToday = useCallback(() => {
    TodayService.Today('')
      .then((data) => {
        setToday(data);
        setError(null);
      })
      .catch((e) => setError(errorText(e)));
  }, []);

  useEffect(loadSidebar, [loadSidebar]);
  useEffect(() => {
    if (section === 'today') loadToday();
  }, [section, loadToday]);

  // Данные могли измениться, пока окно было в фоне: перечитываем при возврате.
  useEffect(() => {
    const onFocus = () => {
      loadSidebar();
      if (section === 'today') loadToday();
    };
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, [loadSidebar, loadToday, section]);

  return (
    <div className="app">
      <Sidebar data={sidebar} section={section} onSelect={setSection} onMonthChange={setMonth} />
      <main className="main">
        {section === 'today'
          ? <TodayScreen data={today} error={error} />
          : <PlaceholderScreen title={SECTION_TITLES[section]} />}
      </main>
    </div>
  );
}
