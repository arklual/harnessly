// «+» of the field: a photo or screenshot (up to 10 MB), a file (up to 50 MB), the earlier and quick prompts, context to
// mention (project, task, design), and — in a chat — what the machine and the team have: a file of the project, a
// design, a document, a board, a merge request or a task. Each of those opens its picker in the same panel (back with
// ←). On a computer it is a small menu over the button; on a phone a sheet from the bottom with large tiles.
import { useCallback, useState, type ChangeEvent } from 'react';
import AttachPicker from '@/next/features/composer/attach-picker';
import {
  ATTACH_KIND,
  type IAttachRow,
  type IAttachScope,
  type TAttachKind,
} from '@/next/features/composer/attach-sources';
import StashList, { type IStash } from '@/next/features/composer/stash-list';
import { stashHint } from '@/next/features/composer/keys';
import type { ComposerContext } from '@/next/features/composer/types';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import Sheet from '@/next/ui/sheet';
import { usePhone } from '@/next/ui/use-media';
import { t } from '@/next/i18n';

export type TFileKind = 'photo' | 'file' | 'camera';

// What each kind asks the system picker for. A photo asks for image/* and not for image/heic by name: the iPhone hands a
// JPEG over for the first and the HEIC original for the second, and the agents read no HEIC. The camera takes one shot.
const PICKER: Record<TFileKind, { accept?: string; multiple: boolean; capture?: 'environment' }> = {
  photo: { accept: 'image/*', multiple: true },
  camera: { accept: 'image/*', multiple: false, capture: 'environment' },
  file: { multiple: true },
};
const KINDS = Object.keys(PICKER) as TFileKind[];

/** The places to attach, in the menu's order, with their icon and words. */
export const ATTACH_ITEMS: readonly { kind: TAttachKind; icon: string; name: string; hint: string }[] = [
  {
    kind: ATTACH_KIND.project,
    icon: 'file-code',
    name: 'next.attach.project',
    hint: 'next.attach.projectHint',
  },
  { kind: ATTACH_KIND.design, icon: 'palette', name: 'next.attach.design', hint: 'next.attach.designHint' },
  { kind: ATTACH_KIND.doc, icon: 'file-text', name: 'next.attach.doc', hint: 'next.attach.docHint' },
  { kind: ATTACH_KIND.board, icon: 'presentation', name: 'next.attach.board', hint: 'next.attach.boardHint' },
  {
    kind: ATTACH_KIND.link,
    icon: 'git-pull-request',
    name: 'next.attach.link',
    hint: 'next.attach.linkHint',
  },
] as const;

/** What a chat's field can attach besides files: where to look, what is already there, and where a pick goes. */
export interface IPlusAttach {
  scope: IAttachScope;
  /** The keys (`kind:id`) of what the field already holds. */
  attached: ReadonlySet<string>;
  onAttach: (row: IAttachRow) => void;
}

interface IPlusMenuProps {
  contexts: ComposerContext[];
  /** The files the person picked; the pickers are in this component, so they stay in the page while the menu closes. */
  onFiles: (files: File[]) => void;
  onContext: (id: string) => void;
  /** Opens the list of earlier and quick prompts over the field. */
  onPrompts: () => void;
  /** The places of the machine and the team; without it «+» offers only files, prompts and contexts. */
  attach?: IPlusAttach | null;
  /** The drafts put aside in this field and the way to put one more; without it «+» has no such items. */
  stash?: IStash | null;
  disabled?: boolean;
}

const PlusMenu = ({
  contexts,
  onFiles,
  onContext,
  onPrompts,
  attach = null,
  stash = null,
  disabled = false,
}: IPlusMenuProps) => {
  const phone = usePhone();
  const [open, setOpen] = useState(false);
  // The picker shown in the panel in place of the menu; null: the menu.
  const [view, setView] = useState<TAttachKind | null>(null);
  // The list of stashed drafts, shown in the panel in the same place.
  const [stashOpen, setStashOpen] = useState(false);
  const [now, setNow] = useState(0);
  const [pickers, setPickers] = useState<Partial<Record<TFileKind, HTMLInputElement | null>>>({});
  const bindPhoto = useCallback(
    (node: HTMLInputElement | null) => setPickers((all) => ({ ...all, photo: node })),
    [],
  );
  const bindCamera = useCallback(
    (node: HTMLInputElement | null) => setPickers((all) => ({ ...all, camera: node })),
    [],
  );
  const bindFile = useCallback(
    (node: HTMLInputElement | null) => setPickers((all) => ({ ...all, file: node })),
    [],
  );
  const binders = { photo: bindPhoto, camera: bindCamera, file: bindFile };
  // Every opening starts at the menu; the picker that was open stays drawn while the panel leaves.
  const toggle = (next: boolean) => {
    if (next) {
      setView(null);
      setStashOpen(false);
      setNow(Date.now());
    }
    setOpen(next);
  };
  const pick = (what: () => void) => () => {
    setOpen(false);
    what();
  };
  // The picker opens inside the tap itself: iOS opens it for nothing else, and an input that is not in the page may
  // never report the choice there.
  const attachFile = (kind: TFileKind) => {
    pickers[kind]?.click();
  };
  const picked = (event: ChangeEvent<HTMLInputElement>) => {
    const input = event.currentTarget;
    const files = [...(input.files ?? [])];
    // The same file may be picked again; the files themselves stay valid.
    input.value = '';
    if (files.length > 0) onFiles(files);
  };
  const inputs = KINDS.map((kind) => (
    <input
      key={kind}
      ref={binders[kind]}
      type="file"
      className="hx-plus__input"
      data-kind={kind}
      accept={PICKER[kind].accept}
      multiple={PICKER[kind].multiple}
      capture={PICKER[kind].capture}
      tabIndex={-1}
      aria-hidden="true"
      onChange={picked}
    />
  ));
  const trigger = (
    <button
      type="button"
      className="hx-round hx-plus-btn"
      data-variant="ghost"
      data-size="sm"
      aria-label={t('next.plus.label')}
      aria-expanded={open}
      disabled={disabled}
      onClick={phone ? () => toggle(true) : undefined}
    >
      <Icon name="plus" className="hx-plus-btn__ic" />
    </button>
  );
  const contextItems = contexts.map((item) => (
    <button
      key={item.id}
      type="button"
      className="hx-plus__item"
      role="menuitem"
      onClick={pick(() => onContext(item.id))}
    >
      <Icon name={item.icon} size="sm" />
      <span className="hx-plus__name">{item.label}</span>
      <Icon name="chevron-right" size="sm" className="hx-plus__chev" />
    </button>
  ));
  const placeItems = attach && (
    <div className="hx-plus__group" role="group" aria-label={t('next.attach.group')}>
      {!phone && <div className="hx-plus__label">{t('next.attach.group')}</div>}
      {ATTACH_ITEMS.map((item) => (
        <button
          key={item.kind}
          type="button"
          className="hx-plus__item"
          role="menuitem"
          data-attach={item.kind}
          onClick={() => setView(item.kind)}
        >
          <Icon name={item.icon} size="sm" />
          <span className="hx-plus__name">{t(item.name)}</span>
          {!phone && <span className="hx-plus__hint">{t(item.hint)}</span>}
          <Icon name="chevron-right" size="sm" className="hx-plus__chev" />
        </button>
      ))}
    </div>
  );
  const stashItems = stash && (
    <>
      <button
        type="button"
        className="hx-plus__item"
        role="menuitem"
        data-stash="put"
        disabled={!stash.canStash}
        onClick={pick(stash.onStash)}
      >
        <Icon name="bookmark-plus" size="sm" />
        <span className="hx-plus__name">{t('next.stash.put')}</span>
        {!phone && <span className="hx-plus__hint">{stashHint()}</span>}
      </button>
      {stash.rows.length > 0 && (
        <button
          type="button"
          className="hx-plus__item"
          role="menuitem"
          data-stash="list"
          onClick={() => setStashOpen(true)}
        >
          <Icon name="history" size="sm" />
          <span className="hx-plus__name">{t('next.stash.list', { n: stash.rows.length })}</span>
          <Icon name="chevron-right" size="sm" className="hx-plus__chev" />
        </button>
      )}
    </>
  );
  const stashPane = stash && stashOpen && (
    <StashList
      rows={stash.rows}
      now={now}
      onBack={() => setStashOpen(false)}
      onRestore={(id) => {
        setOpen(false);
        stash.onRestore(id);
      }}
      onDrop={(id) => {
        stash.onDrop(id);
        if (stash.rows.length <= 1) setStashOpen(false);
      }}
    />
  );
  const picker =
    stashPane ||
    (attach && view && (
      <AttachPicker
        key={view}
        kind={view}
        scope={attach.scope}
        attached={attach.attached}
        onBack={() => setView(null)}
        onPick={(row) => {
          setOpen(false);
          attach.onAttach(row);
        }}
      />
    ));

  if (phone) {
    const menu = (
      <>
        <div className="hx-plus__tiles" role="menu">
          <button
            type="button"
            className="hx-plus__tile"
            role="menuitem"
            onClick={pick(() => attachFile('photo'))}
          >
            <Icon name="image" />
            {t('next.plus.photosFiles')}
          </button>
          <button
            type="button"
            className="hx-plus__tile"
            role="menuitem"
            onClick={pick(() => attachFile('camera'))}
          >
            <Icon name="camera" />
            {t('next.plus.camera')}
          </button>
          <button
            type="button"
            className="hx-plus__tile"
            role="menuitem"
            onClick={pick(() => attachFile('file'))}
          >
            <Icon name="paperclip" />
            {t('next.plus.file')}
          </button>
        </div>
        {placeItems}
        <button type="button" className="hx-plus__item" role="menuitem" onClick={pick(onPrompts)}>
          <Icon name="history" size="sm" />
          <span className="hx-plus__name">{t('next.plus.prompts')}</span>
        </button>
        {stashItems}
        {contextItems.length > 0 && <div className="hx-plus__sep" />}
        {contextItems}
      </>
    );
    return (
      <>
        {trigger}
        {inputs}
        <Sheet
          open={open}
          onClose={() => setOpen(false)}
          label={t('next.plus.label')}
          className="hx-plus-sheet"
        >
          {picker || menu}
        </Sheet>
      </>
    );
  }

  const menu = (
    <div className="hx-plus__pane" role="menu">
      {attach && <div className="hx-plus__label">{t('next.attach.computer')}</div>}
      <button
        type="button"
        className="hx-plus__item"
        role="menuitem"
        onClick={pick(() => attachFile('photo'))}
      >
        <Icon name="image" size="sm" />
        <span className="hx-plus__name">{t('next.plus.photo')}</span>
        <span className="hx-plus__hint">{t('next.plus.photoLimit')}</span>
      </button>
      <button
        type="button"
        className="hx-plus__item"
        role="menuitem"
        onClick={pick(() => attachFile('file'))}
      >
        <Icon name="paperclip" size="sm" />
        <span className="hx-plus__name">{t(attach ? 'next.attach.computerFile' : 'next.plus.file')}</span>
        <span className="hx-plus__hint">{t('next.plus.fileLimit')}</span>
      </button>
      {placeItems && <div className="hx-plus__sep" />}
      {placeItems}
      <div className="hx-plus__sep" />
      <button type="button" className="hx-plus__item" role="menuitem" onClick={pick(onPrompts)}>
        <Icon name="history" size="sm" />
        <span className="hx-plus__name">{t('next.plus.prompts')}</span>
        <span className="hx-plus__hint">{t('next.plus.promptsHint')}</span>
      </button>
      {stashItems}
      {contextItems.length > 0 && <div className="hx-plus__sep" />}
      {contextItems}
    </div>
  );
  return (
    <>
      {inputs}
      <Popover
        trigger={trigger}
        hint={t('next.plus.tip')}
        open={open}
        onOpenChange={toggle}
        label={t('next.plus.label')}
        className={picker ? 'hx-plus hx-plus--pick' : 'hx-plus'}
      >
        {picker || menu}
      </Popover>
    </>
  );
};

export default PlusMenu;
