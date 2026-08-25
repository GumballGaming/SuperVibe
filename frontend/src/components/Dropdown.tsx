import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { Check, ChevronDown } from "lucide-react";

export interface DropdownOption {
  value: string;
  label: ReactNode;
  disabled?: boolean;
}

interface DropdownProps {
  value: string;
  options: DropdownOption[];
  onChange: (value: string) => void;
  disabled?: boolean;
  className?: string;
  style?: CSSProperties;
  id?: string;
  ariaLabel?: string;
}

type MenuPosition = {
  left: number;
  top: number;
  width: number;
  maxHeight: number;
};

function nextEnabledIndex(options: DropdownOption[], current: number, direction: 1 | -1): number {
  if (options.length === 0) return -1;
  let index = current;
  for (let i = 0; i < options.length; i += 1) {
    index = (index + direction + options.length) % options.length;
    if (!options[index].disabled) return index;
  }
  return -1;
}

export default function Dropdown({
  value,
  options,
  onChange,
  disabled = false,
  className,
  style,
  id,
  ariaLabel,
}: DropdownProps) {
  const generatedId = useId().replace(/:/g, "");
  const triggerId = id || `dropdown-${generatedId}`;
  const listboxId = `${triggerId}-listbox`;
  const rootRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const [menuPosition, setMenuPosition] = useState<MenuPosition | null>(null);

  const selectedIndex = options.findIndex((option) => option.value === value);
  const selectedOption = selectedIndex >= 0 ? options[selectedIndex] : undefined;

  const updatePosition = useCallback(() => {
    const trigger = triggerRef.current;
    if (!trigger) return;
    const rect = trigger.getBoundingClientRect();
    const margin = 8;
    const gap = 4;
    const estimatedHeight = Math.min(280, Math.max(44, options.length * 36 + 8));
    const roomBelow = window.innerHeight - rect.bottom - margin - gap;
    const roomAbove = rect.top - margin - gap;
    const openUp = roomBelow < estimatedHeight && roomAbove > roomBelow;
    const maxHeight = Math.max(44, openUp ? roomAbove : roomBelow);
    const width = Math.min(Math.max(rect.width, 180), window.innerWidth - margin * 2);
    const left = Math.min(Math.max(margin, rect.left), window.innerWidth - width - margin);
    const top = openUp ? Math.max(margin, rect.top - Math.min(estimatedHeight, maxHeight) - gap) : rect.bottom + gap;
    setMenuPosition({ left, top, width, maxHeight });
  }, [options.length]);

  useLayoutEffect(() => {
    if (open) updatePosition();
  }, [open, updatePosition]);

  useEffect(() => {
    if (!open) return;
    const selected = options.findIndex((option) => option.value === value && !option.disabled);
    setHighlightedIndex(selected >= 0 ? selected : nextEnabledIndex(options, -1, 1));
  }, [open, options, value]);

  useEffect(() => {
    if (!open) return;
    const closeIfOutside = (event: Event) => {
      const target = event.target as Node;
      if (!rootRef.current?.contains(target) && !menuRef.current?.contains(target)) setOpen(false);
    };
    document.addEventListener("pointerdown", closeIfOutside);
    document.addEventListener("focusin", closeIfOutside);
    return () => {
      document.removeEventListener("pointerdown", closeIfOutside);
      document.removeEventListener("focusin", closeIfOutside);
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const reposition = () => updatePosition();
    window.addEventListener("resize", reposition);
    document.addEventListener("scroll", reposition, true);
    return () => {
      window.removeEventListener("resize", reposition);
      document.removeEventListener("scroll", reposition, true);
    };
  }, [open, updatePosition]);

  useEffect(() => {
    if (!open || highlightedIndex < 0) return;
    menuRef.current?.querySelector<HTMLElement>(`[data-dropdown-index="${highlightedIndex}"]`)?.scrollIntoView({ block: "nearest" });
  }, [highlightedIndex, open]);

  const choose = (option: DropdownOption) => {
    if (option.disabled) return;
    onChange(option.value);
    setOpen(false);
    triggerRef.current?.focus();
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLButtonElement>) => {
    if (disabled) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (!open) {
        setOpen(true);
        return;
      }
      setHighlightedIndex((current) => nextEnabledIndex(options, current < 0 ? (event.key === "ArrowDown" ? -1 : 0) : current, event.key === "ArrowDown" ? 1 : -1));
      return;
    }
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      if (!open) {
        setOpen(true);
        return;
      }
      const option = options[highlightedIndex];
      if (option) choose(option);
      return;
    }
    if (event.key === "Escape" && open) {
      event.preventDefault();
      setOpen(false);
    }
  };

  return (
    <div ref={rootRef} className={`dropdown${className ? ` ${className}` : ""}`} style={style}>
      <button
        ref={triggerRef}
        id={triggerId}
        type="button"
        className="dropdown__trigger"
        disabled={disabled}
        role="combobox"
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listboxId}
        aria-activedescendant={open && highlightedIndex >= 0 ? `${listboxId}-${highlightedIndex}` : undefined}
        onClick={() => !disabled && setOpen((current) => !current)}
        onKeyDown={handleKeyDown}
      >
        <span className="dropdown__value">{selectedOption?.label ?? value}</span>
        <ChevronDown className="dropdown__chevron" size={13} aria-hidden="true" />
      </button>
      {open && menuPosition &&
        createPortal(
          <div
            ref={menuRef}
            id={listboxId}
            className="dropdown__menu"
            role="listbox"
            aria-labelledby={triggerId}
            style={{
              left: menuPosition.left,
              top: menuPosition.top,
              width: menuPosition.width,
              maxHeight: menuPosition.maxHeight,
            }}
          >
            {options.length === 0 ? (
              <div className="dropdown__empty">No options</div>
            ) : (
              options.map((option, index) => {
                const selected = option.value === value;
                const highlighted = index === highlightedIndex;
                return (
                  <button
                    key={`${option.value}-${index}`}
                    id={`${listboxId}-${index}`}
                    type="button"
                    className={`dropdown__option${selected ? " dropdown__option--selected" : ""}${highlighted ? " dropdown__option--highlighted" : ""}`}
                    role="option"
                    aria-selected={selected}
                    disabled={option.disabled}
                    data-dropdown-index={index}
                    onMouseEnter={() => !option.disabled && setHighlightedIndex(index)}
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => choose(option)}
                  >
                    <span className="dropdown__option-label">{option.label}</span>
                    {selected && <Check size={13} aria-hidden="true" />}
                  </button>
                );
              })
            )}
          </div>,
          document.body,
        )}
    </div>
  );
}
