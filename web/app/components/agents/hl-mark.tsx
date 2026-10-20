/** The product mark: "h." on a dark tile, the same drawing as public/logo.svg. */
const HlMark = ({ className }: { className?: string }) => (
  <svg className={className} viewBox="0 0 1024 1024" aria-hidden="true" focusable="false">
    <rect width="1024" height="1024" rx="224" fill="#0B0D10" />
    <path
      transform="translate(235.1 712.2)"
      d="M29.12 -400.4H135.72V0H29.12ZM244.4 -305.76Q287.56 -305.76 317.46 -288.6Q347.36 -271.44 363.48 -240.24Q379.6 -209.04 379.6 -166.4V0H273V-150.8Q273 -184.08 256.1 -201.76Q239.2 -219.44 207.48 -219.44Q185.64 -219.44 169.26 -210.08Q152.88 -200.72 144.3 -183.56Q135.72 -166.4 135.72 -143L102.44 -160.16Q108.68 -207.48 128.96 -239.98Q149.24 -272.48 179.14 -289.12Q209.04 -305.76 244.4 -305.76Z"
      fill="#F2F2EE"
    />
    <circle cx="715.6" cy="668" r="44.2" fill="#D4FF3A" />
  </svg>
);

export default HlMark;
