import { createRoot } from "react-dom/client";
import "@fontsource-variable/inter";
import "@fontsource-variable/jetbrains-mono";
import "./styles/global.css";
import App from "./App";

// No StrictMode: its development-only double mount/unmount would start, close
// and re-subscribe every terminal effect, which spawns and kills real shells
// (and drops their first output). Terminal effects are not idempotent.
const root = createRoot(document.getElementById("root")!);
root.render(<App />);
