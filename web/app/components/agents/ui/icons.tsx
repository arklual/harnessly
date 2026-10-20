/** Inline glyphs used inside the primitives, so the kit does not depend on an icon package. */

export const TwCheckIcon = () => (
  <svg className="tw-ui-icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
    <path
      d="M3.5 8.5l3 3 6-7"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

export const TwChevronRightIcon = () => (
  <svg className="tw-ui-icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
    <path
      d="M6 3.5l4.5 4.5L6 12.5"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

export const TwChevronLeftIcon = () => (
  <svg className="tw-ui-icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
    <path
      d="M10 3.5L5.5 8l4.5 4.5"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

export const TwChevronDownIcon = () => (
  <svg className="tw-ui-icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
    <path
      d="M3.5 6l4.5 4.5L12.5 6"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

export const TwSpinner = ({ label }: { label?: string }) => (
  <svg
    className="tw-ui-spinner"
    viewBox="0 0 16 16"
    role={label ? 'img' : undefined}
    aria-label={label}
    aria-hidden={label ? undefined : true}
    focusable="false"
  >
    <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" strokeWidth="2" strokeOpacity="0.25" />
    <path d="M14 8a6 6 0 0 0-6-6" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
  </svg>
);
