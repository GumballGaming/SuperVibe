export const THEME_OPTIONS = [
  { value: "system", label: "System", description: "Follow your operating system" },
  { value: "dark", label: "Dark", description: "The current SuperVibe look" },
  { value: "dim", label: "Dim", description: "Softer contrast for long sessions" },
  { value: "light", label: "Light", description: "Bright workspace for daytime" },
] as const;

export const ACCENT_OPTIONS = [
  { value: "orange", label: "Ember", color: "#ff6b3d" },
  { value: "violet", label: "Violet", color: "#9b8afb" },
  { value: "blue", label: "Blue", color: "#5ea7ff" },
  { value: "green", label: "Mint", color: "#4cc38a" },
] as const;

const THEME_VALUES: Record<string, true> = {
  system: true,
  dark: true,
  dim: true,
  light: true,
};
const ACCENT_VALUES: Record<string, true> = {
  orange: true,
  violet: true,
  blue: true,
  green: true,
};

export function applyTheme(theme: string | undefined, accent: string | undefined): void {
  const root = document.documentElement;
  root.dataset.theme = theme && THEME_VALUES[theme] ? theme : "dark";
  root.dataset.accent = accent && ACCENT_VALUES[accent] ? accent : "orange";
}
