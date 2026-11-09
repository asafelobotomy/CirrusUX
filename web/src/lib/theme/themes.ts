// Built-in themes. A theme is only token values; components never hard-code
// colours. In the real panel these live as themes/*.toml on the gateway.

export const TOKEN_GROUPS = {
  Surfaces: ['bg', 'surface', 'surface-2', 'surface-3', 'border', 'border-strong'],
  Text: ['text', 'text-muted', 'text-faint'],
  Accent: ['accent', 'on-accent'],
  State: ['ok', 'warn', 'bad', 'info', 'sleep'],
} as const;

export type TokenName = (typeof TOKEN_GROUPS)[keyof typeof TOKEN_GROUPS][number];
export const TOKEN_NAMES = Object.values(TOKEN_GROUPS).flat() as TokenName[];

export interface Theme {
  id: string;
  name: string;
  scheme: 'dark' | 'light';
  builtIn?: boolean;
  /** Corner radius in px, part of the theme's character. */
  radius: number;
  tokens: Record<TokenName, string>;
}

export const BUILT_IN_THEMES: Theme[] = [
  {
    id: 'graphite',
    name: 'Graphite',
    scheme: 'dark',
    builtIn: true,
    radius: 14,
    tokens: {
      bg: '#0f1115',
      surface: '#161920',
      'surface-2': '#1d2129',
      'surface-3': '#262b35',
      border: '#252a33',
      'border-strong': '#363c48',
      text: '#e8eaef',
      'text-muted': '#a0a7b4',
      'text-faint': '#6c7380',
      accent: '#7c9cff',
      'on-accent': '#0b1020',
      ok: '#3ecf8e',
      warn: '#f2b54f',
      bad: '#ff6b6b',
      info: '#6cb6ff',
      sleep: '#9b8cff',
    },
  },
  {
    id: 'paper',
    name: 'Paper',
    scheme: 'light',
    builtIn: true,
    radius: 14,
    tokens: {
      bg: '#f4f4f1',
      surface: '#ffffff',
      'surface-2': '#f3f3ef',
      'surface-3': '#e9e9e4',
      border: '#e3e3dd',
      'border-strong': '#cfcfc7',
      text: '#1b1d22',
      'text-muted': '#555c68',
      'text-faint': '#8a909a',
      accent: '#3b5bdb',
      'on-accent': '#ffffff',
      ok: '#18864f',
      warn: '#a86a0b',
      bad: '#cf3a3a',
      info: '#1f6fbf',
      sleep: '#6a50d3',
    },
  },
  {
    id: 'midnight',
    name: 'Midnight',
    scheme: 'dark',
    builtIn: true,
    radius: 18,
    tokens: {
      bg: '#000000',
      surface: '#0a0a0c',
      'surface-2': '#121215',
      'surface-3': '#1b1b20',
      border: '#18181c',
      'border-strong': '#2a2a31',
      text: '#f1f1f4',
      'text-muted': '#9d9da8',
      'text-faint': '#62626c',
      accent: '#4fe0c4',
      'on-accent': '#00221c',
      ok: '#4fe08a',
      warn: '#ffc457',
      bad: '#ff6e7a',
      info: '#62b8ff',
      sleep: '#a395ff',
    },
  },
  {
    id: 'dusk',
    name: 'Dusk',
    scheme: 'dark',
    builtIn: true,
    radius: 12,
    tokens: {
      bg: '#1a1714',
      surface: '#221e1a',
      'surface-2': '#2a2520',
      'surface-3': '#342e28',
      border: '#2f2924',
      'border-strong': '#433b33',
      text: '#ede4d8',
      'text-muted': '#b2a698',
      'text-faint': '#7d7366',
      accent: '#f0a868',
      'on-accent': '#2a1606',
      ok: '#9ccf7f',
      warn: '#f2c35b',
      bad: '#f07c6c',
      info: '#8fbbe0',
      sleep: '#c2a5f0',
    },
  },
];
