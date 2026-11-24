import type { TagView } from '../../bindings/daily-agent/internal/app/models';

type Props = {
  tag: TagView;
  /** sidebar — крупный тег в боковой панели, row — в строке задачи, meta — в подписи входящей. */
  variant: 'sidebar' | 'row' | 'meta';
};

export function TagChip({ tag, variant }: Props) {
  return (
    <span className={`tag-chip tag-chip--${variant}`}>
      <span className="tag-dot" style={{ background: tag.color }} aria-hidden="true" />
      {tag.name}
    </span>
  );
}
