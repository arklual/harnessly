// «Обучение» (/learn): the lessons in three sections. Each is done by hand — the tour leads through the real
// interface, on the person's own chats and the Relay demo projects — or watched as a short film recorded from the same
// interface. A lesson done to its end is marked passed.
import { useState } from 'react';
import LessonPlayer from '@/next/features/learn/lesson-player';
import { LEARN_SECTIONS, LESSONS, posterOf, type ILesson } from '@/next/features/learn/lessons';
import { t } from '@/next/i18n';
import { doLesson } from '@/next/features/learn/do-lesson';
import { useTour } from '@/next/stores/tour';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import '@/next/styles/learn.css';

const filmLabel = (lesson: ILesson) => t('next.learn.film', { sec: String(Math.round(lesson.dur)) });

interface ILessonCardProps {
  lesson: ILesson;
  passed: boolean;
  onWatch: () => void;
}

const LessonCard = ({ lesson, passed, onWatch }: ILessonCardProps) => {
  const title = t(`next.learn.${lesson.id}.title`);
  return (
    <article className="hx-learn-card">
      <button
        type="button"
        className="hx-learn-card__media"
        aria-label={t('next.learn.watch', { title })}
        onClick={onWatch}
      >
        <img src={posterOf(lesson.id)} alt="" loading="lazy" />
        <span className="hx-learn-card__play" aria-hidden="true">
          <Icon name="play" />
        </span>
      </button>
      <h3 className="hx-learn-card__name">
        {title}
        {passed && (
          <span className="hx-learn-card__passed" title={t('next.learn.passed')}>
            <Icon name="circle-check" size="sm" />
          </span>
        )}
      </h3>
      <p className="hx-learn-card__desc">{t(`next.learn.${lesson.id}.desc`)}</p>
      <div className="hx-learn-card__acts">
        <button type="button" className="hx-learn-chip" data-k="do" onClick={() => doLesson(lesson.id)}>
          <Icon name="square-mouse-pointer" size="sm" />
          {t('next.learn.do')}
        </button>
        <button type="button" className="hx-learn-chip" onClick={onWatch}>
          <Icon name="play" size="sm" />
          {filmLabel(lesson)}
        </button>
      </div>
    </article>
  );
};

interface ILearnPageProps {
  onOpenDrawer: () => void;
}

const LearnPage = ({ onOpenDrawer }: ILearnPageProps) => {
  const tour = useTour();
  const [watching, setWatching] = useState<string | null>(null);
  const first = LESSONS[0] as ILesson;

  return (
    <div className="hx-learn">
      <div className="hx-learn__bar">
        <button
          type="button"
          className="hx-head-btn hx-learn__menu"
          aria-label={t('next.header.openMenu')}
          onClick={onOpenDrawer}
        >
          <Icon name="menu" />
        </button>
      </div>
      <div className="hx-learn__in">
        <header className="hx-learn__head">
          <h1 className="hx-learn__title">{t('next.learn.title')}</h1>
          <p className="hx-learn__sub">{t('next.learn.sub')}</p>
        </header>

        <section className="hx-learn-hero">
          <button
            type="button"
            className="hx-learn-hero__media"
            aria-label={t('next.learn.watch', { title: t(`next.learn.${first.id}.title`) })}
            onClick={() => setWatching(first.id)}
          >
            <img src={posterOf(first.id)} alt="" />
            <span className="hx-learn-card__play" aria-hidden="true">
              <Icon name="play" />
            </span>
          </button>
          <div className="hx-learn-hero__txt">
            <span className="hx-learn__eyebrow">{t('next.learn.start')}</span>
            <h2 className="hx-learn-hero__title">{t(`next.learn.${first.id}.title`)}</h2>
            <p className="hx-learn-hero__desc">{t(`next.learn.${first.id}.desc`)}</p>
            <div className="hx-learn-hero__acts">
              <Button variant="primary" icon="square-mouse-pointer" onClick={() => doLesson(first.id)}>
                {t('next.learn.doLong')}
              </Button>
              <Button variant="ghost" icon="play" onClick={() => setWatching(first.id)}>
                {filmLabel(first)}
              </Button>
            </div>
          </div>
        </section>

        {LEARN_SECTIONS.map((section) => (
          <section key={section} className="hx-learn-sec">
            <h2 className="hx-learn-sec__title">{t(`next.learn.section.${section}`)}</h2>
            <p className="hx-learn-sec__sub">{t(`next.learn.section.${section}.sub`)}</p>
            <div className="hx-learn-grid">
              {LESSONS.filter((l) => l.section === section).map((lesson) => (
                <LessonCard
                  key={lesson.id}
                  lesson={lesson}
                  passed={tour.passed.includes(lesson.id)}
                  onWatch={() => setWatching(lesson.id)}
                />
              ))}
            </div>
          </section>
        ))}
      </div>
      <LessonPlayer id={watching} onOpen={setWatching} onClose={() => setWatching(null)} />
    </div>
  );
};

export default LearnPage;
