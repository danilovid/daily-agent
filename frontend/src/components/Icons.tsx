// Иконки из макетов. Цвет берётся из currentColor, размер — из CSS-переменных.

import type { ReactNode } from 'react';

type IconProps = { size?: string; strokeWidth?: number };

function Svg({ size = 'var(--icon-md)', strokeWidth = 1.8, children }: IconProps & { children: ReactNode }) {
  return (
    <svg
      style={{ width: size, height: size, flexShrink: 0 }}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

export const SearchIcon = (p: IconProps) => (
  <Svg {...p}><circle cx="11" cy="11" r="6.5" /><path d="M16 16l4 4" /></Svg>
);

export const CalendarIcon = (p: IconProps) => (
  <Svg size="var(--icon-nav)" {...p}>
    <rect x="4" y="5" width="16" height="15" rx="3" /><path d="M8 3v4" /><path d="M16 3v4" /><path d="M4 10h16" />
  </Svg>
);

export const InboxIcon = (p: IconProps) => (
  <Svg size="var(--icon-nav)" {...p}>
    <path d="M4 13l2.5-7h11L20 13v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1z" /><path d="M4 13h4.5a3.5 3.5 0 0 0 7 0H20" />
  </Svg>
);

export const ListIcon = (p: IconProps) => (
  <Svg size="var(--icon-nav)" {...p}>
    <path d="M9 7h11" /><path d="M9 12h11" /><path d="M9 17h11" /><path d="M4 7h.01" /><path d="M4 12h.01" /><path d="M4 17h.01" />
  </Svg>
);

export const ClockIcon = (p: IconProps) => (
  <Svg size="var(--icon-nav)" {...p}><circle cx="12" cy="12" r="8" /><path d="M12 8v4l2.5 2.5" /></Svg>
);

export const ChatIcon = (p: IconProps) => (
  <Svg size="var(--icon-nav)" {...p}>
    <path d="M5 5h14a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1h-7l-4 3v-3H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z" />
  </Svg>
);

export const SettingsIcon = (p: IconProps) => (
  <Svg size="var(--icon-nav)" {...p}>
    <path d="M4 7h10" /><path d="M18 7h2" /><circle cx="16" cy="7" r="2" /><path d="M4 17h2" /><path d="M10 17h10" /><circle cx="8" cy="17" r="2" />
  </Svg>
);

export const PlusIcon = (p: IconProps) => (
  <Svg strokeWidth={2} {...p}><path d="M12 5v14" /><path d="M5 12h14" /></Svg>
);

export const ChevronLeftIcon = (p: IconProps) => (
  <Svg strokeWidth={2} {...p}><path d="M15 5l-7 7 7 7" /></Svg>
);

export const ChevronRightIcon = (p: IconProps) => (
  <Svg strokeWidth={2} {...p}><path d="M9 5l7 7-7 7" /></Svg>
);

export const CheckIcon = (p: IconProps) => (
  <Svg size="var(--icon-lg)" strokeWidth={2} {...p}><path d="M5 12.5l4.5 4.5L19 7.5" /></Svg>
);

export const CloseIcon = (p: IconProps) => (
  <Svg size="var(--icon-lg)" {...p}><path d="M6 6l12 12" /><path d="M18 6L6 18" /></Svg>
);

export const VideoIcon = (p: IconProps) => (
  <Svg size="var(--icon-sm)" strokeWidth={2} {...p}>
    <rect x="3" y="7" width="12" height="10" rx="2" /><path d="M15 11l5-3v8l-5-3" />
  </Svg>
);

export const ExternalIcon = (p: IconProps) => (
  <Svg size="var(--icon-xs)" strokeWidth={2} {...p}>
    <path d="M14 5h5v5" /><path d="M19 5l-8 8" /><path d="M18 14v4a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h4" />
  </Svg>
);

export const ChevronDownIcon = (p: IconProps) => (
  <Svg size="var(--icon-chevron)" strokeWidth={2} {...p}><path d="M6 9l6 6 6-6" /></Svg>
);

export const SmallCheckIcon = (p: IconProps) => (
  <Svg size="var(--icon-xs)" strokeWidth={2.4} {...p}><path d="M5 12.5l4.5 4.5L19 7.5" /></Svg>
);

export const ExpandIcon = (p: IconProps) => (
  <Svg size="var(--icon-nav)" {...p}>
    <path d="M14 5h5v5" /><path d="M19 5l-6 6" /><path d="M10 19H5v-5" /><path d="M5 19l6-6" />
  </Svg>
);

export const SendIcon = (p: IconProps) => (
  <Svg strokeWidth={2.2} {...p}><path d="M12 19V5" /><path d="M6 11l6-6 6 6" /></Svg>
);

export const StopIcon = (p: IconProps) => (
  <Svg {...p}><rect x="7" y="7" width="10" height="10" rx="1.5" fill="currentColor" stroke="none" /></Svg>
);

export const MoreIcon = (p: IconProps) => (
  <Svg size="var(--icon-lg)" strokeWidth={2.6} {...p}><path d="M6 12h.01" /><path d="M12 12h.01" /><path d="M18 12h.01" /></Svg>
);

export const NoteIcon = (p: IconProps) => (
  <Svg size="var(--icon-note)" {...p}><circle cx="12" cy="12" r="8" /><path d="M12 11v5" /><path d="M12 8h.01" /></Svg>
);

export const ToolIcon = (p: IconProps) => (
  <Svg size="var(--icon-note)" {...p}><circle cx="11" cy="11" r="6.5" /><path d="M16 16l4 4" /></Svg>
);

export const UpDownIcon = (p: IconProps) => (
  <Svg strokeWidth={2.2} {...p}><path d="M8 10l4-4 4 4" /><path d="M8 14l4 4 4-4" /></Svg>
);
