// The app launcher's tiles (PLAN §4.3). Apps the app store installed come
// from its own records: name, port, scheme, icon and colour, no guessing.
// Any other container that publishes a port gets a tile too, named and
// coloured from a small list of well-known apps.

export interface AppTile {
  /** The container's name. */
  id: string;
  name: string;
  port: number;
  https: boolean;
  icon: string;
  color: string;
  group: string;
}

interface Known { name: string; icon: string; color: string; group: string; https?: boolean }

/** Well-known apps by their base container name. */
const KNOWN: Record<string, Known> = {
  jellyfin: { name: 'Jellyfin', icon: 'Clapperboard', color: '#a35ee8', group: 'Watch & listen' },
  plex: { name: 'Plex', icon: 'Clapperboard', color: '#e5a00d', group: 'Watch & listen' },
  emby: { name: 'Emby', icon: 'Clapperboard', color: '#52b54b', group: 'Watch & listen' },
  navidrome: { name: 'Navidrome', icon: 'Music', color: '#3fa7ff', group: 'Watch & listen' },
  audiobookshelf: { name: 'Audiobookshelf', icon: 'Headphones', color: '#e9a23b', group: 'Watch & listen' },
  kavita: { name: 'Kavita', icon: 'BookOpen', color: '#4cc38a', group: 'Watch & listen' },
  tinymediamanager: { name: 'tinyMediaManager', icon: 'Film', color: '#e56b6f', group: 'Manage' },
  'musicbrainz-picard': { name: 'Picard', icon: 'Tags', color: '#8d7cf6', group: 'Manage', https: true },
  picard: { name: 'Picard', icon: 'Tags', color: '#8d7cf6', group: 'Manage', https: true },
  nextcloud: { name: 'Nextcloud', icon: 'Cloud', color: '#0082c9', group: 'Files' },
  immich: { name: 'Immich', icon: 'Image', color: '#4250af', group: 'Files' },
  syncthing: { name: 'Syncthing', icon: 'RefreshCw', color: '#0891d1', group: 'Files' },
  'home-assistant': { name: 'Home Assistant', icon: 'House', color: '#18bcf2', group: 'Home' },
  homeassistant: { name: 'Home Assistant', icon: 'House', color: '#18bcf2', group: 'Home' },
  vaultwarden: { name: 'Vaultwarden', icon: 'Lock', color: '#175ddc', group: 'Home' },
};

const PALETTE = ['#5b8def', '#4cc38a', '#e9a23b', '#a35ee8', '#e56b6f', '#3fa7ff', '#8d7cf6'];

/** "jellyfin-1" (a compose replica) → "jellyfin"; 1Panel's "1Panel-jellyfin-a1B2" too. */
export function baseName(container: string): string {
  let base = container;
  if (/^1Panel-/i.test(base)) base = base.slice('1Panel-'.length).replace(/-[A-Za-z0-9]{4}$/, '');
  return base.replace(/-\d+$/, '').toLowerCase();
}

/** "jellyfin-1" → "Jellyfin"; unknown names are capitalised. */
export function appName(container: string): string {
  const base = baseName(container);
  return KNOWN[base]?.name ?? base.charAt(0).toUpperCase() + base.slice(1);
}

function hash(s: string): number {
  let h = 0;
  for (const c of s) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return h;
}

/** Tiles for installed apps and other containers that publish a port, by name. */
export function tilesFor(
  containers: { name: string; port: number | null }[],
  installed: { id: string; name: string; mainPort: number; https?: boolean }[] = [],
  catalogue: { id: string; icon: string; color: string; group: string }[] = [],
): AppTile[] {
  const fromStore: AppTile[] = installed.map((a) => {
    const c = catalogue.find((x) => x.id === a.id);
    const k = KNOWN[a.id];
    return {
      id: a.id, name: a.name, port: a.mainPort, https: !!a.https,
      icon: c?.icon ?? k?.icon ?? 'Package', color: c?.color ?? k?.color ?? PALETTE[hash(a.id) % PALETTE.length],
      group: c?.group ?? k?.group ?? 'Apps',
    };
  });
  const others = containers
    .filter((c) => c.port && !installed.some((a) => a.id === c.name))
    .map((c) => {
      const k = KNOWN[baseName(c.name)];
      return {
        id: c.name,
        name: appName(c.name),
        port: c.port!,
        https: k?.https ?? false,
        icon: k?.icon ?? 'Package',
        color: k?.color ?? PALETTE[hash(c.name) % PALETTE.length],
        group: k?.group ?? 'Apps',
      };
    });
  return [...fromStore, ...others].sort((a, b) => a.name.localeCompare(b.name));
}
