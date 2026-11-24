import { useEffect, type ReactNode } from 'react';

type Props = {
  /** Название окна для экранного диктора. */
  label: string;
  onClose: () => void;
  /** Закрывать по клику мимо окна. Выключается, когда в карточке есть несохранённые правки. */
  dismissOnOverlay?: boolean;
  size?: 'md' | 'sm';
  children: ReactNode;
};

/** Модальное окно поверх затемнённого экрана. Esc закрывает его. */
export function Modal({ label, onClose, dismissOnOverlay = true, size = 'md', children }: Props) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [onClose]);

  return (
    <div
      className="modal-overlay"
      onMouseDown={(e) => {
        if (dismissOnOverlay && e.target === e.currentTarget) onClose();
      }}
    >
      <section role="dialog" aria-modal="true" aria-label={label} className={`modal modal--${size}`}>
        {children}
      </section>
    </div>
  );
}
