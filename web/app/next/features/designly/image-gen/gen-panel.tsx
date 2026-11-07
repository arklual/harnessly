// «Сгенерировать картинку»: a short brief, how many variants, and the drawing of them into the selected element of a
// board. The deck's toolbar and the canvas's card both show this panel; what it does to a board is the host's `apply`.
// While a picture is drawn the board shows a veil on the element; a single variant is written as soon as it is drawn,
// several are shown as a strip to choose from. An element drawn earlier remembers its brief (to draw it again) and its
// job (the other variants).
import { useRef, useState } from 'react';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import Segmented from '@/next/ui/segmented';
import { toast } from '@/next/features/designly/sdk/toast';
import {
  GEN_VARIANTS,
  readGenVariants,
  shapeOf,
  thumbUrl,
  type IGenPic,
} from '@/next/features/designly/image-gen/api';
import {
  GEN_PHASE,
  chooseGen,
  dismissGen,
  genKey,
  offerGen,
  startGen,
  useGenDone,
  useGenRun,
  type IGenDone,
  type IGenRun,
  type TGenApply,
} from '@/next/features/designly/image-gen/gen-store';

/** The element a picture is drawn into, as the board told it. */
export interface IImageTarget {
  /** `genKey(artifact, board, tpl)`. */
  key: string;
  artifact: string;
  tpl: number;
  /** Width over height. */
  ratio: number;
  /** What the board says it was drawn from (`data-gen-prompt`, `data-gen-job`). */
  gen: IGenDone | null;
  /** What an empty place says it is for (`data-image-prompt`): the field starts with it. */
  brief?: string;
}

/** The element a picture is drawn into, from what the board said of its selection. */
export const imageTargetOf = (
  artifact: string,
  board: string,
  sel: { tpl: number; ratio: number; gen: IGenDone | null },
): IImageTarget => ({
  key: genKey(artifact, board, sel.tpl),
  artifact,
  tpl: sel.tpl,
  ratio: sel.ratio,
  gen: sel.gen,
});

export interface IImageGenPanelProps {
  machine: MachineId;
  target: IImageTarget;
  apply: TGenApply;
  /** Inside a card rather than a toolbar's popover. */
  embedded?: boolean;
}

const variantOptions = GEN_VARIANTS.map((n) => ({ value: String(n), label: String(n) }));

const Strip = ({ run, machine }: { run: IGenRun; machine: MachineId }) => {
  const waiting = Math.max(0, run.count - run.pics.length);
  const drawing = run.phase === GEN_PHASE.drawing;
  return (
    <div className="hx-imagegen__strip" role="group" aria-label={t('next.imagegen.strip')}>
      {run.pics.map((pic) => (
        <button
          key={pic.src}
          type="button"
          className="hx-imagegen__tile"
          disabled={run.applying}
          aria-label={t('next.imagegen.pick', { n: pic.n })}
          onClick={() => void chooseGen(run.key, pic)}
        >
          <img src={thumbUrl(machine, run.artifact, pic.src)} alt="" draggable={false} />
        </button>
      ))}
      {run.count > 1 &&
        drawing &&
        Array.from({ length: waiting }, (_, i) => (
          <span key={`w${i}`} className="hx-imagegen__tile" data-wait="1" />
        ))}
    </div>
  );
};

const RunView = ({ run, machine, retry }: { run: IGenRun; machine: MachineId; retry: () => void }) => {
  if (run.phase === GEN_PHASE.failed) {
    return (
      <div className="hx-imagegen__run" role="alert">
        <span className="hx-imagegen__said">{run.error ?? t('next.imagegen.failed')}</span>
        <span className="hx-imagegen__acts">
          <button type="button" className="hx-imagegen__btn" onClick={retry}>
            {t('next.imagegen.retry')}
          </button>
          <button
            type="button"
            className="hx-imagegen__x"
            aria-label={t('next.imagegen.close')}
            onClick={() => dismissGen(run.key)}
          >
            <Icon name="x" size="sm" />
          </button>
        </span>
      </div>
    );
  }
  const drawing = run.phase === GEN_PHASE.drawing;
  return (
    <div className="hx-imagegen__run">
      <div className="hx-imagegen__line">
        <span className="hx-imagegen__said" data-live={drawing ? '1' : undefined}>
          {drawing ? t('next.imagegen.drawing') : t('next.imagegen.choose')}
        </span>
        <button
          type="button"
          className="hx-imagegen__x"
          aria-label={t('next.imagegen.cancel')}
          onClick={() => dismissGen(run.key)}
        >
          <Icon name="x" size="sm" />
        </button>
      </div>
      {(run.count > 1 || !drawing) && <Strip run={run} machine={machine} />}
    </div>
  );
};

export const ImageGenPanel = ({ machine, target, apply, embedded = false }: IImageGenPanelProps) => {
  const run = useGenRun(target.key);
  const done = useGenDone(target.key);
  const gen = done ?? target.gen;
  const [prompt, setPrompt] = useState(gen?.prompt ?? target.brief ?? '');
  const [count, setCount] = useState(1);
  /** The variants of an earlier job are being fetched. */
  const [fetching, setFetching] = useState(false);
  const field = useRef<HTMLTextAreaElement>(null);
  const brief = prompt.trim();

  const make = () => {
    if (brief === '' || run !== null) return;
    void startGen(
      machine,
      target.key,
      { artifact: target.artifact, prompt: brief, count, shape: shapeOf(target.ratio) },
      target.tpl,
      apply,
    );
  };
  const retry = () => {
    if (run === null) return;
    void startGen(
      machine,
      target.key,
      { artifact: target.artifact, prompt: run.prompt, count: run.count, shape: shapeOf(target.ratio) },
      target.tpl,
      apply,
    );
  };
  const others = () => {
    if (!gen || fetching) return;
    setFetching(true);
    readGenVariants(machine, target.artifact, gen.job).then(
      (pics: IGenPic[]) => {
        setFetching(false);
        if (pics.length === 0) {
          toast(t('next.imagegen.noOthers'));
          return;
        }
        offerGen(target.key, target.artifact, target.tpl, gen.prompt, gen.job, pics, apply);
      },
      (failure: Error) => {
        setFetching(false);
        toast(t('next.imagegen.othersFailed'), { tone: 'error', description: failure.message });
      },
    );
  };

  return (
    <div className="hx-imagegen" data-embedded={embedded ? '1' : undefined}>
      {run === null ? (
        <>
          <textarea
            ref={field}
            className="hx-imagegen__field"
            rows={2}
            value={prompt}
            maxLength={1000}
            placeholder={t('next.imagegen.placeholder')}
            aria-label={t('next.imagegen.prompt')}
            onChange={(event) => setPrompt(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault();
                make();
              }
            }}
          />
          <div className="hx-imagegen__row">
            <Segmented
              label={t('next.imagegen.variants')}
              value={String(count)}
              options={variantOptions}
              onChange={(value) => setCount(Number(value))}
            />
            <span className="hx-imagegen__acts">
              {gen && (
                <button
                  type="button"
                  className="hx-imagegen__btn"
                  data-busy={fetching ? 'true' : undefined}
                  disabled={fetching}
                  onClick={others}
                >
                  {t('next.imagegen.others')}
                </button>
              )}
              <button
                type="button"
                className="hx-imagegen__btn"
                data-primary="1"
                disabled={brief === ''}
                onClick={make}
              >
                <Icon name={gen ? 'refresh-cw' : 'sparkles'} size="sm" />
                {gen ? t('next.imagegen.again') : t('next.imagegen.make')}
              </button>
            </span>
          </div>
        </>
      ) : (
        <RunView run={run} machine={machine} retry={retry} />
      )}
    </div>
  );
};

export default ImageGenPanel;
