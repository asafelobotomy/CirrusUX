// Widget catalogue (PLAN §4.4). `live` widgets dim and show "as of" while
// cloud64 sleeps; `bare` widgets draw their own card content edge to edge.
import type { Component } from 'svelte';
import * as I from '../icons';
import PowerWidget from './PowerWidget.svelte';
import AppsWidget from './AppsWidget.svelte';
import TimelineWidget from './TimelineWidget.svelte';
import ContainersWidget from './ContainersWidget.svelte';
import BackupsWidget from './BackupsWidget.svelte';
import SecurityWidget from './SecurityWidget.svelte';
import StorageWidget from './StorageWidget.svelte';
import TempsWidget from './TempsWidget.svelte';
import GrokWidget from './GrokWidget.svelte';
import ChangelogWidget from './ChangelogWidget.svelte';
import UpdatesWidget from './UpdatesWidget.svelte';
import NoteWidget from './NoteWidget.svelte';
import DigestWidget from './DigestWidget.svelte';

export interface WidgetMeta {
  title: string;
  description: string;
  component: Component<any>;
  icon: Component;
  size: [number, number];
  min: [number, number];
  live?: boolean;
  bare?: boolean;
  config?: Record<string, string>;
}

export const WIDGETS: Record<string, WidgetMeta> = {
  power: { title: 'Power', description: 'Awake or asleep, sleep countdown, wake and stay awake', component: PowerWidget, icon: I.Power, size: [5, 4], min: [3, 3], bare: true },
  apps: { title: 'Apps', description: 'Launcher tiles with live status', component: AppsWidget, icon: I.LayoutDashboard, size: [6, 2], min: [2, 2], live: true },
  timeline: { title: 'Sleep timeline', description: "Today's awake and asleep periods with reasons", component: TimelineWidget, icon: I.CalendarClock, size: [7, 2], min: [4, 2] },
  containers: { title: 'Containers', description: 'Health, pinned tags and restart', component: ContainersWidget, icon: I.Package, size: [5, 4], min: [3, 3], live: true },
  backups: { title: 'Backups', description: 'Last run, next run, run now', component: BackupsWidget, icon: I.Archive, size: [4, 2], min: [3, 2] },
  security: { title: 'Security', description: 'ClamAV, firewall lockdown, fail2ban', component: SecurityWidget, icon: I.ShieldCheck, size: [3, 2], min: [3, 2], live: true },
  storage: { title: 'Storage', description: 'Media disk usage per folder', component: StorageWidget, icon: I.HardDrive, size: [4, 3], min: [3, 2], live: true },
  temps: { title: 'Temperature', description: 'CPU temperature over 2 hours', component: TempsWidget, icon: I.Thermometer, size: [3, 2], min: [2, 2], live: true },
  grok: { title: 'Ask Grok', description: 'Quick question with suggestions', component: GrokWidget, icon: I.Sparkles, size: [5, 2], min: [3, 2] },
  changelog: { title: 'Changelog', description: 'Recent actions with undo', component: ChangelogWidget, icon: I.History, size: [6, 3], min: [3, 2] },
  updates: { title: 'Updates', description: 'Pending updates waiting for approval', component: UpdatesWidget, icon: I.RefreshCw, size: [4, 2], min: [3, 2] },
  digest: { title: 'Daily digest', description: 'Morning summary built from cached data', component: DigestWidget, icon: I.Sunrise, size: [6, 3], min: [3, 2] },
  note: { title: 'Note', description: 'A short note of your own', component: NoteWidget, icon: I.FileText, size: [4, 2], min: [2, 2], config: { text: '' } },
};
