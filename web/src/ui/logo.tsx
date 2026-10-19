/** The project's mark, "h." (public/logo.svg; docs/brand). */
export function Logo({ size = 24 }: { size?: number }) {
  return <img src="./logo.svg" width={size} height={size} alt="" className="logo" />;
}
