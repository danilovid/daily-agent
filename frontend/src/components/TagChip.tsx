import type { TagView } from '../../bindings/daily-agent/internal/app/models';

type Props = {
  tag: TagView;
  /** sidebar — крупный тег в боковой панели, row — в строке задачи, meta — в подписи входящей. */
  variant: 'sidebar' | 'row' | 'meta';
  /** Тег выбран: фильтр в боковой панели. */
  selected?: boolean;
};

export function TagChip({ tag, variant, selected }: Props) {
  return (
    <span className={`tag-chip tag-chip--${variant}${selected ? ' tag-chip--selected' : ''}`}>
      <span className="tag-dot" style={{ background: tag.color }} aria-hidden="true" />
      {tag.name}
    </span>
  );
}
