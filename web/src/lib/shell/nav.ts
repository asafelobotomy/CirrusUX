import type { Component } from 'svelte';
import * as I from '../icons';
import type { Route } from '../state/ui.svelte';

export interface NavItem { route: Route; label: string; icon: Component; phase?: string }

export const MAIN_NAV: NavItem[] = [
  { route: 'home', label: 'Home', icon: I.House },
  { route: 'services', label: 'Services', icon: I.Package },
  { route: 'backups', label: 'Backups', icon: I.Archive },
  { route: 'storage', label: 'Storage', icon: I.HardDrive, phase: 'Phase 3' },
  { route: 'security', label: 'Security', icon: I.ShieldCheck },
  { route: 'updates', label: 'Updates', icon: I.RefreshCw, phase: 'Phase 3' },
  { route: 'media', label: 'Media', icon: I.Film, phase: 'Phase 3' },
  { route: 'logs', label: 'Logs', icon: I.ScrollText, phase: 'Phase 3' },
  { route: 'changelog', label: 'Changelog', icon: I.History },
];

export const SECONDARY_NAV: NavItem[] = [
  { route: 'settings', label: 'Settings', icon: I.Settings },
  { route: 'theme-editor', label: 'Theme editor', icon: I.Palette },
  { route: 'gallery', label: 'Components', icon: I.Component },
];

export const ALL_NAV = [...MAIN_NAV, ...SECONDARY_NAV];
