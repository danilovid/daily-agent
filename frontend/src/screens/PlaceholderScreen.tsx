type Props = { title: string };

/** Заглушка раздела, у которого ещё нет готового экрана. */
export function PlaceholderScreen({ title }: Props) {
  return (
    <>
      <div className="screen-header drag-region">
        <div className="screen-header__title">
          <h2 className="screen-title">{title}</h2>
        </div>
      </div>
      <p className="placeholder">Раздел «{title}» появится в следующих шагах.</p>
    </>
  );
}
