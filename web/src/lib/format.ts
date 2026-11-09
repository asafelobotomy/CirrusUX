// Sizes as the panel shows them: "554 GB", "1.9 TB", "25 MB", "4 kB".
// Decimal units, as disks are sold. `precise` keeps one more figure
// ("2.0 GB", "1.92 TB") where the exact size matters, as on Storage.
// `small` is for files: bytes under 1 kB ("96 B"), then one decimal under
// 10 kB and for MB ("2.0 kB", "52 kB", "1.0 MB").

export function fmtBytes(bytes: number, precise = false, small = false): string {
  if (small) {
    if (bytes < 1000) return `${bytes} B`;
    if (bytes < 1e6) return `${(bytes / 1e3).toFixed(bytes < 1e4 ? 1 : 0)} kB`;
    if (bytes < 1e9) return `${(bytes / 1e6).toFixed(1)} MB`;
  }
  if (bytes >= 1e12) return `${(bytes / 1e12).toFixed(precise ? 2 : 1)} TB`;
  if (bytes >= 1e9) return precise ? `${(bytes / 1e9).toFixed(1)} GB` : `${Math.round(bytes / 1e9)} GB`;
  if (bytes >= 1e6) return `${Math.round(bytes / 1e6)} MB`;
  return `${Math.round(bytes / 1e3)} kB`;
}

/** The same, for a size the panel already holds in GB. */
export const fmtGb = (gb: number, precise = false) => fmtBytes(gb * 1e9, precise);

/** A count with thousands separators, in the panel's locale. */
export const fmtNum = (n: number) => n.toLocaleString('en-GB');
