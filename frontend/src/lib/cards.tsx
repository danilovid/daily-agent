// Открытие карточек задачи и встречи и экрана настроек с любого экрана.

import { createContext, useContext } from 'react';

export type OpenCard =
  | { kind: 'task'; id?: string }
  | { kind: 'meeting'; id?: string }
  | { kind: 'new-tag' };

export type CardsContextValue = {
  /** Открыть карточку задачи. Без id — новая задача. */
  openTask: (id?: string) => void;
  /** Открыть карточку встречи. Без id — новая встреча. */
  openMeeting: (id?: string) => void;
  /** Открыть экран «Настройки», например из чата, когда не задана модель. */
  openSettings: () => void;
  /** Открыть маленький чат с этим чатом, например после «Составить план». */
  openChat: (id: string) => void;
};

export const CardsContext = createContext<CardsContextValue | null>(null);

export function useCards(): CardsContextValue {
  const ctx = useContext(CardsContext);
  if (!ctx) throw new Error('useCards вне CardsContext');
  return ctx;
}
