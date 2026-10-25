// An icon of the design: a Lucide glyph drawn as a CSS mask (styles/icons.css), its colour is currentColor.
export type TIconName = string;

interface IIconProps {
  name: TIconName;
  size?: 'sm' | 'md';
  className?: string;
}

const Icon = ({ name, size = 'md', className }: IIconProps) => (
  <i
    className={className ? `hx-icon ${className}` : 'hx-icon'}
    data-i={name}
    data-size={size === 'sm' ? 'sm' : undefined}
    aria-hidden="true"
  />
);

export default Icon;
