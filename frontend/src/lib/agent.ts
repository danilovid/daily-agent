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

/** Почему агенту сейчас нельзя написать, или null, если можно. */
export function agentUnavailable(s: AgentStatusView | null): string | null {
  switch (s?.state) {
    case 'ready':
      return s.modelConfigured ? null : 'Модель агента не настроена';
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
export function agentSettingsHint(s: AgentStatusView | null): string | null {
  if (s?.state === 'ready' && !s.modelConfigured) return 'Чтобы писать агенту, задайте модель в настройках.';
  if (s?.state === 'failed') return 'OpenCode не запускается. Подробности — в настройках.';
  return null;
}
