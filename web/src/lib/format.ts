// Sizes as the panel shows them: "554 GB", "1.9 TB", "25 MB", "4 kB".
// Decimal units, as disks are sold. `precise` keeps one more figure
// ("2.0 GB", "1.92 TB") where the exact size matters, as on Storage.

export function fmtBytes(bytes: number, precise = false): string {
  if (bytes >= 1e12) return `${(bytes / 1e12).toFixed(precise ? 2 : 1)} TB`;
  if (bytes >= 1e9) return precise ? `${(bytes / 1e9).toFixed(1)} GB` : `${Math.round(bytes / 1e9)} GB`;
  if (bytes >= 1e6) return `${Math.round(bytes / 1e6)} MB`;
  return `${Math.round(bytes / 1e3)} kB`;
}

/** The same, for a size the panel already holds in GB. */
export const fmtGb = (gb: number, precise = false) => fmtBytes(gb * 1e9, precise);
