import { Events } from '@wailsio/runtime';
// Общее состояние данных: номер версии, по которому экраны перечитывают
// данные после изменений, и показ ошибок.

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';

type DataContextValue = {
  /** Растёт после каждого изменения данных. */
  version: number;
  /** Сообщить, что данные изменились: экраны перечитают их. */
  refresh: () => void;
  /** Показать ошибку внизу окна. */
  showError: (e: unknown) => void;
  error: string | null;
};

const DataContext = createContext<DataContextValue | null>(null);

/** Текст ошибки для окна. Ядро пишет ошибки со строчной буквы, в окне — с заглавной. */
export function errorText(e: unknown): string {
  let text: string;
  if (e instanceof Error) text = e.message;
  else if (typeof e === 'object' && e !== null && 'message' in e) text = String((e as { message: unknown }).message);
  else text = String(e);
  return text.charAt(0).toUpperCase() + text.slice(1);
}

export function DataProvider({ children }: { children: ReactNode }) {
  const [version, setVersion] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const timer = useRef<number | undefined>(undefined);

  const refresh = useCallback(() => setVersion((v) => v + 1), []);
  // Базу изменил другой процесс: свой MCP-сервер или внешний агент.
  useEffect(() => Events.On('data:changed', () => refresh()), [refresh]);
  const showError = useCallback((e: unknown) => {
    setError(errorText(e));
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setError(null), 6000);
  }, []);

  const value = useMemo(() => ({ version, refresh, showError, error }), [version, refresh, showError, error]);
  return <DataContext.Provider value={value}>{children}</DataContext.Provider>;
}

export function useData(): DataContextValue {
  const ctx = useContext(DataContext);
  if (!ctx) throw new Error('useData вне DataProvider');
  return ctx;
}

/**
 * Загрузить данные экрана. Перезагружает их, когда меняются deps или
 * данные в приложении, и при возврате к окну. Устаревшие ответы отбрасывает.
 */
export function useLoad<T>(load: () => Promise<T>, deps: unknown[]): { data: T | null; error: string | null } {
  const { version } = useData();
  const key = JSON.stringify(deps);
  const [state, setState] = useState<{ key: string; data: T | null; error: string | null }>({ key, data: null, error: null });
  const [focusTick, setFocusTick] = useState(0);

  useEffect(() => {
    const onFocus = () => setFocusTick((t) => t + 1);
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, []);

  useEffect(() => {
    let alive = true;
    load()
      .then((data) => alive && setState({ key, data, error: null }))
      .catch((e) => alive && setState((s) => ({ key, data: s.key === key ? s.data : null, error: errorText(e) })));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [version, focusTick, ...deps]);

  // Данные для прежних deps (другая вкладка, другой фильтр) не показываем.
  // При обновлении тех же deps прежние данные видны до прихода новых.
  if (state.key !== key) return { data: null, error: null };
  return { data: state.data, error: state.error };
}

/**
 * Выполнить действие, которое меняет данные: при успехе перечитать экраны,
 * при ошибке показать её.
 */
export function useAction() {
  const { refresh, showError } = useData();
  return useCallback(
    async (action: () => Promise<unknown>) => {
      try {
        await action();
        refresh();
        return true;
      } catch (e) {
        showError(e);
        return false;
      }
    },
    [refresh, showError],
  );
}
