// SPDX-License-Identifier: Apache-2.0
import { forwardRef, type HTMLAttributes, type ReactNode, type Ref } from 'react';

import { classNames } from '../internal/cx';

import '../viz.css';

export interface VizCardProps extends Omit<HTMLAttributes<HTMLElement>, 'title'> {
  /** Название блока. */
  readonly title?: ReactNode;
  /** Рядом с названием: «?» с подсказкой, бейдж «бета». */
  readonly info?: ReactNode;
  /** Подпись под названием: период, источник. */
  readonly description?: ReactNode;
  /** Справа в шапке: переключатели, меню, зум. На телефоне переносятся под название. */
  readonly actions?: ReactNode;
  /** Под шапкой: легенда, фильтры. */
  readonly legend?: ReactNode;
  /** Под графиком: полоса «весь период», сноски. */
  readonly footer?: ReactNode;
  /** `plain` — без рамки и фона, когда блок уже стоит в карточке страницы. */
  readonly variant?: 'card' | 'plain';
  readonly children: ReactNode;
}

/**
 * Блок доски: шапка (название, «?», действия), легенда, тело, подвал. Общий каркас для любых
 * графиков аналитики — волн, столбиков, долей — чтобы все блоки доски были одинаковой формы.
 */
export const VizCard = forwardRef<HTMLElement, VizCardProps>(function VizCard(
  { title, info, description, actions, legend, footer, variant = 'card', className, children, ...rest },
  ref,
) {
  const body = (
    <>
      <header className="viz-card__header">
        <div className="viz-card__heading">
          <div className="viz-card__title-row">
            <h3 className="viz-card__title">{title}</h3>
            {info ? <span className="viz-card__info">{info}</span> : null}
          </div>
          {description ? <p className="viz-card__description">{description}</p> : null}
        </div>
        {actions ? <div className="viz-card__actions">{actions}</div> : null}
      </header>
      {legend ? <div className="viz-card__legend">{legend}</div> : null}
      <div className="viz-card__body">{children}</div>
      {footer ? <footer className="viz-card__footer">{footer}</footer> : null}
    </>
  );
  if (variant === 'plain') {
    return (
      <section
        ref={ref}
        className={classNames('viz-root', 'viz-card', 'viz-card--plain', className)}
        {...rest}
      >
        {body}
      </section>
    );
  }
  return (
    <div
      ref={ref as Ref<HTMLDivElement>}
      role="region"
      className={classNames('viz-root', 'viz-surface', 'viz-card', className)}
      {...(rest as HTMLAttributes<HTMLDivElement>)}
    >
      {body}
    </div>
  );
});
