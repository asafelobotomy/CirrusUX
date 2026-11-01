// WCAG contrast helpers used to keep a user-picked accent readable.

type RGB = [number, number, number];

function hexToRgb(hex: string): RGB {
  let h = hex.replace('#', '').trim();
  if (h.length === 3) h = [...h].map((c) => c + c).join('');
  const n = parseInt(h.slice(0, 6), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function rgbToHex([r, g, b]: RGB): string {
  return '#' + [r, g, b].map((v) => Math.round(v).toString(16).padStart(2, '0')).join('');
}

function luminance([r, g, b]: RGB): number {
  const lin = (v: number) => {
    v /= 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

export function contrast(a: string, b: string): number {
  const la = luminance(hexToRgb(a));
  const lb = luminance(hexToRgb(b));
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

function rgbToHsl([r, g, b]: RGB): [number, number, number] {
  r /= 255; g /= 255; b /= 255;
  const max = Math.max(r, g, b), min = Math.min(r, g, b);
  const l = (max + min) / 2;
  if (max === min) return [0, 0, l];
  const d = max - min;
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  const h = max === r ? (g - b) / d + (g < b ? 6 : 0) : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
  return [h / 6, s, l];
}

function hslToRgb([h, s, l]: [number, number, number]): RGB {
  if (s === 0) return [l * 255, l * 255, l * 255];
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const f = (t: number) => {
    if (t < 0) t += 1;
    if (t > 1) t -= 1;
    if (t < 1 / 6) return p + (q - p) * 6 * t;
    if (t < 1 / 2) return q;
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
    return p;
  };
  return [f(h + 1 / 3) * 255, f(h) * 255, f(h - 1 / 3) * 255];
}

/**
 * Nudge `color`'s lightness until it reaches `min` contrast against `bg`.
 * Returns the colour unchanged when it already passes.
 */
export function ensureContrast(color: string, bg: string, min = 4.5): { color: string; adjusted: boolean } {
  if (contrast(color, bg) >= min) return { color, adjusted: false };
  const [h, s, l0] = rgbToHsl(hexToRgb(color));
  const lighter = luminance(hexToRgb(bg)) < 0.5;
  for (let i = 1; i <= 100; i++) {
    const l = Math.min(1, Math.max(0, l0 + (lighter ? i : -i) / 100));
    const c = rgbToHex(hslToRgb([h, s, l]));
    if (contrast(c, bg) >= min) return { color: c, adjusted: true };
  }
  return { color: lighter ? '#ffffff' : '#000000', adjusted: true };
}

/** Pick dark or light text for a filled accent button. */
export function onColor(color: string): string {
  return contrast(color, '#0b0d12') >= contrast(color, '#ffffff') ? '#0b0d12' : '#ffffff';
}
