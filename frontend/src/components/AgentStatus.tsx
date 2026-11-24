import { useEffect, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { AgentService } from '../../bindings/daily-agent/internal/app';
import type { AgentStatusView } from '../../bindings/daily-agent/internal/app';

/** Состояние OpenCode: при открытии окна и по событию agent:status. */
function useAgentStatus(): AgentStatusView | null {
  const [status, setStatus] = useState<AgentStatusView | null>(null);
  useEffect(() => {
    AgentService.Status().then(setStatus).catch(() => setStatus(null));
    const off = Events.On('agent:status', (event) => setStatus(event.data as AgentStatusView));
    return () => off();
  }, []);
  return status;
}

function describe(s: AgentStatusView | null): { text: string; tone: 'online' | 'pending' | 'error' | 'off' } {
  switch (s?.state) {
    case 'ready':
      return {
        text: s.modelConfigured ? `OpenCode на связи · ${s.since}` : 'OpenCode на связи · модель не настроена',
        tone: s.modelConfigured ? 'online' : 'pending',
      };
    case 'starting':
      return { text: 'OpenCode запускается…', tone: 'pending' };
    case 'restarting':
      return { text: 'OpenCode перезапускается…', tone: 'pending' };
    case 'failed':
      return { text: 'OpenCode недоступен', tone: 'error' };
    default:
      return { text: 'Агент не подключён', tone: 'off' };
  }
}

/** Строка состояния агента в подвале боковой панели. Подробности — во всплывающей подсказке. */
export function AgentStatus() {
  const status = useAgentStatus();
  const { text, tone } = describe(status);
  const details = status?.error || (status?.version ? `OpenCode ${status.version}` : undefined);
  return (
    <p className="agent-status" title={details} aria-live="polite">
      <span className={`agent-status__dot agent-status__dot--${tone}`} aria-hidden="true" />
      <span className="agent-status__text">{text}</span>
      {details && <span className="visually-hidden">{details}</span>}
    </p>
  );
}
