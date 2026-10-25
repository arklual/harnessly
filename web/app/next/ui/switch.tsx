// An on/off switch, after .hx-switch of the design.
interface ISwitchProps {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
}

const Switch = ({ label, checked, onChange, disabled = false }: ISwitchProps) => (
  <button
    type="button"
    role="switch"
    className="hx-switch"
    aria-label={label}
    aria-checked={checked}
    disabled={disabled}
    onClick={() => onChange(!checked)}
  >
    <i />
  </button>
);

export default Switch;
