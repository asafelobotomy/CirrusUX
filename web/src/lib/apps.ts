// The app launcher's tiles (PLAN §4.3) and how container names map to app
// names. Real configuration, used in live and demo mode alike.

export interface AppTile {
  id: string;
  name: string;
  port: number;
  https?: boolean;
  icon: string;
  color: string;
  /** Runs in a container whose app name (see appName) matches `name`. */
  inContainer?: boolean;
  group: string;
}

export const apps: AppTile[] = [
  { id: 'jellyfin', name: 'Jellyfin', port: 8096, icon: 'Clapperboard', color: '#a35ee8', inContainer: true, group: 'Watch & listen' },
  { id: 'navidrome', name: 'Navidrome', port: 40108, icon: 'Music', color: '#3fa7ff', inContainer: true, group: 'Watch & listen' },
  { id: 'abs', name: 'Audiobookshelf', port: 40096, icon: 'Headphones', color: '#e9a23b', inContainer: true, group: 'Watch & listen' },
  { id: 'kavita', name: 'Kavita', port: 5000, icon: 'BookOpen', color: '#4cc38a', inContainer: true, group: 'Watch & listen' },
  { id: 'tmm', name: 'tinyMediaManager', port: 4000, icon: 'Film', color: '#e56b6f', inContainer: true, group: 'Manage' },
  { id: 'picard', name: 'Picard', port: 5800, https: true, icon: 'Tags', color: '#8d7cf6', inContainer: true, group: 'Manage' },
  { id: '1panel', name: '1Panel', port: 10086, icon: 'LayoutPanelLeft', color: '#5b8def', group: 'Manage' },
];

const NAMES: Record<string, string> = {
  jellyfin: 'Jellyfin', navidrome: 'Navidrome', audiobookshelf: 'Audiobookshelf', kavita: 'Kavita',
  tinymediamanager: 'tinyMediaManager', 'musicbrainz-picard': 'Picard', openresty: 'OpenResty',
};

/** "1Panel-jellyfin-a1B2" → "Jellyfin", "musicbrainz-picard-1" → "Picard". */
export function appName(container: string): string {
  const base = container.replace(/^1Panel-/i, '').replace(/-[A-Za-z0-9]{4}$/, '').replace(/-\d+$/, '').toLowerCase();
  return NAMES[base] ?? base.charAt(0).toUpperCase() + base.slice(1);
}
