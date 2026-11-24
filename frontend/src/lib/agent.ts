// Состояние агента: при открытии окна и по событию agent:status.

import { useEffect, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { AgentService } from '../../bindings/daily-agent/internal/app';
import type { AgentStatusView } from '../../bindings/daily-agent/internal/app';

export function useAgentStatus(): AgentStatusView | null {
  const [status, setStatus] = useState<AgentStatusView | null>(null);
  useEffect(() => {
    AgentService.Status().then(setStatus).catch(() => setStatus(null));
    const off = Events.On('agent:status', (event) => setStatus(event.data as AgentStatusView));
    return () => off();
  }, []);
  return status;
}

/** Почему агенту сейчас нельзя написать в рабочий или личный чат, или null, если можно. */
export function agentUnavailable(s: AgentStatusView | null, personal = false): string | null {
  switch (s?.state) {
    case 'ready':
      if (personal) return s.personalConfigured ? null : 'Модель для личных задач не выбрана';
      return s.modelConfigured ? null : 'Модель для рабочих задач не выбрана';
    case 'starting':
      return 'OpenCode запускается…';
    case 'restarting':
      return 'OpenCode перезапускается…';
    case 'failed':
      return 'OpenCode недоступен';
    default:
      return 'Агент не подключён';
  }
}

/**
 * Подсказка, если агенту нельзя писать, пока не поправить настройки:
 * не задана модель или OpenCode не запускается. Иначе null.
 */
export function agentSettingsHint(s: AgentStatusView | null, personal = false): string | null {
  if (s?.state === 'ready' && !(personal ? s.personalConfigured : s.modelConfigured)) {
    return personal
      ? 'Чтобы писать в личный чат, выберите модель для личных задач в настройках.'
      : 'Чтобы писать агенту, выберите модель для рабочих задач в настройках.';
  }
  if (s?.state === 'failed') return 'OpenCode не запускается. Подробности — в настройках.';
  return null;
}
