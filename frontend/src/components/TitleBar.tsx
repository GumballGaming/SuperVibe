import { Minus, Square, X, Zap } from "lucide-react";
import { windowControls } from "../lib/backend";

export default function TitleBar() {
  const controls = windowControls();
  return (
    <div className="titlebar">
      <div className="titlebar__brand">
        <Zap size={14} className="titlebar__logo-icon" />
        SuperVibe
      </div>
      <div className="titlebar__controls">
        <button className="titlebar__btn" onClick={controls.minimise} title="Minimize">
          <Minus size={14} />
        </button>
        <button className="titlebar__btn" onClick={controls.toggleMaximise} title="Maximize">
          <Square size={11.5} />
        </button>
        <button className="titlebar__btn titlebar__btn--close" onClick={controls.quit} title="Close">
          <X size={15} />
        </button>
      </div>
    </div>
  );
}
