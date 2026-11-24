// Поля карточек: выбор даты и выбор из списка в виде кнопки из макета.

import { useRef, type ReactNode } from 'react';
import { ChevronDownIcon, CloseIcon } from './Icons';

type FieldProps = { label: string; children: ReactNode; htmlFor?: string };

/** Подпись и поле под ней. */
export function Field({ label, children, htmlFor }: FieldProps) {
  return (
    <div className="field">
      {htmlFor ? <label className="field__label" htmlFor={htmlFor}>{label}</label> : <h3 className="field__label">{label}</h3>}
      {children}
    </div>
  );
}

type DateFieldProps = {
  value: string;
  display: string;
  ariaLabel: string;
  onChange: (value: string) => void;
  /** Показать крестик, который очищает значение. */
  onClear?: () => void;
  disabled?: boolean;
};

/**
 * Кнопка с датой. По нажатию открывается системный выбор даты: поле
 * input type="date" спрятано под кнопкой.
 */
export function DateField({ value, display, ariaLabel, onChange, onClear, disabled }: DateFieldProps) {
  const input = useRef<HTMLInputElement>(null);
  const open = () => {
    const el = input.current;
    if (!el) return;
    try {
      el.showPicker();
    } catch {
      el.focus();
    }
  };
  return (
    <div className="picker">
      <button type="button" className="picker__control" onClick={open} disabled={disabled} aria-label={`${ariaLabel}: ${display}`}>
        <span>{display}</span>
        {!(onClear && value) && <span className="picker__chevron"><ChevronDownIcon /></span>}
      </button>
      {onClear && value && !disabled && (
        <button type="button" className="picker__clear" aria-label="Убрать срок" onClick={onClear}>
          <CloseIcon size="var(--icon-chevron)" />
        </button>
      )}
      <input
        ref={input}
        type="date"
        className="picker__native"
        tabIndex={-1}
        aria-hidden="true"
        value={value}
        onChange={(e) => e.target.value && onChange(e.target.value)}
      />
    </div>
  );
}

type SelectFieldProps<T extends string | number> = {
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
  ariaLabel: string;
  disabled?: boolean;
};

/** Выбор из списка в виде кнопки из макета. Список — системный. */
export function SelectField<T extends string | number>({ value, options, onChange, ariaLabel, disabled }: SelectFieldProps<T>) {
  return (
    <div className="picker">
      <select
        className="picker__control picker__select"
        aria-label={ariaLabel}
        value={String(value)}
        disabled={disabled}
        onChange={(e) => {
          const picked = options.find((o) => String(o.value) === e.target.value);
          if (picked) onChange(picked.value);
        }}
      >
        {options.map((o) => <option key={String(o.value)} value={String(o.value)}>{o.label}</option>)}
      </select>
      <span className="picker__chevron picker__chevron--overlay"><ChevronDownIcon /></span>
    </div>
  );
}
