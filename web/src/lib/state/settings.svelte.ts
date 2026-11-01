// Appearance settings and theme resolution (PLAN §4.2, §4.5 levels 1, 3, 4).
import { BUILT_IN_THEMES, TOKEN_NAMES, type Theme } from '../theme/themes';
import { ensureContrast, onColor } from '../theme/contrast';
import { load, save } from '../fake/store';

export type Density = 'comfortable' | 'compact';

export interface Wallpaper {
  kind: 'none' | 'gradient' | 'image';
  value: string;
  blur: number;
  dim: number;
}

export const GRADIENTS = [
  { id: 'aurora', css: 'radial-gradient(1200px 700px at 10% -10%, #3b4fd8 0%, transparent 60%), radial-gradient(900px 600px at 100% 0%, #7b3fd1 0%, transparent 55%), radial-gradient(1000px 800px at 50% 120%, #0f8f88 0%, transparent 60%), #0b0d14' },
  { id: 'ember', css: 'radial-gradient(1100px 700px at 0% 0%, #b4532a 0%, transparent 60%), radial-gradient(900px 700px at 100% 100%, #6b2a5c 0%, transparent 60%), #140d0b' },
  { id: 'tide', css: 'linear-gradient(160deg, #0c2b3d 0%, #10485a 45%, #1a6a6a 100%)' },
  { id: 'dawn', css: 'linear-gradient(160deg, #fde2c4 0%, #f6c7d6 50%, #cdd6fb 100%)' },
  { id: 'slate', css: 'linear-gradient(180deg, #20242c 0%, #12141a 100%)' },
];

interface Settings {
  themeId: string;
  followSystem: boolean;
  lightThemeId: string;
  darkThemeId: string;
  accent: string | null;
  density: Density;
  textScale: number;
  wallpaper: Wallpaper;
  customCss: string;
  customThemes: Theme[];
}

const DEFAULTS: Settings = {
  themeId: 'graphite',
  followSystem: false,
  lightThemeId: 'paper',
  darkThemeId: 'graphite',
  accent: null,
  density: 'comfortable',
  textScale: 1,
  wallpaper: { kind: 'none', value: '', blur: 24, dim: 0.35 },
  customCss: '',
  customThemes: [],
};

export const settings: Settings = $state(load('settings', DEFAULTS));

/** Theme being edited in the theme editor; previewed live while set. */
export const editor = $state<{ draft: Theme | null }>({ draft: null });

const systemDark = typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: dark)') : null;
const system = $state({ dark: systemDark?.matches ?? true });
systemDark?.addEventListener('change', (e) => (system.dark = e.matches));

export function allThemes(): Theme[] {
  return [...BUILT_IN_THEMES, ...settings.customThemes];
}

export function themeById(id: string): Theme {
  return allThemes().find((t) => t.id === id) ?? BUILT_IN_THEMES[0];
}

export function activeTheme(): Theme {
  if (editor.draft) return editor.draft;
  if (settings.followSystem) return themeById(system.dark ? settings.darkThemeId : settings.lightThemeId);
  return themeById(settings.themeId);
}

/** Accent actually used: the user's pick, nudged until readable on the theme. */
export function effectiveAccent(theme: Theme = activeTheme()) {
  const wanted = settings.accent && !editor.draft ? settings.accent : theme.tokens.accent;
  const { color, adjusted } = ensureContrast(wanted, theme.tokens.surface, 4.5);
  return { wanted, color, adjusted };
}

export function themeStyle(theme: Theme, accent = theme.tokens.accent): string {
  const vars = TOKEN_NAMES.map((n) => `--${n}: ${n === 'accent' ? accent : theme.tokens[n]}`);
  if (accent !== theme.tokens.accent) vars.push(`--on-accent: ${onColor(accent)}`);
  vars.push(`--radius: ${theme.radius}px`, `color-scheme: ${theme.scheme}`);
  return vars.join('; ');
}

export function applyTheme(): void {
  const theme = activeTheme();
  const { color } = effectiveAccent(theme);
  const root = document.documentElement;
  root.setAttribute('style', themeStyle(theme, color));
  root.style.setProperty('--density', settings.density === 'compact' ? '0.82' : '1');
  root.style.setProperty('--text-scale', String(settings.textScale));
  root.dataset.scheme = theme.scheme;
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme.tokens.bg);
}

$effect.root(() => {
  $effect(() => save('settings', $state.snapshot(settings)));
});
