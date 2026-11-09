// CirrusStore's catalogue as the gateway sends it: the built-in apps
// (internal/catalogue) merged with each source's (internal/gateway/cirrusstore.go).

export interface CatalogueApp {
  id: string;
  name: string;
  description: string;
  website: string;
  icon: string;
  color: string;
  group: string;
  notes?: string;
  image: string;
  tag: string;
  digest: string;
  ports: { name: string; container: number; host: number; protocol?: string; https?: boolean; main?: boolean }[];
  volumes: { name: string; path: string; kind: 'config' | 'cache' | 'data' | 'media'; writable?: boolean; label?: string }[];
  devices?: { path: string; optional?: boolean; why: string; hostGroups?: string[] }[];
  env?: { name: string; value?: string; choices?: string[]; default?: string; label?: string }[];
  runAsUser?: boolean;
  check?: { port: string; path: string };
  minMemoryMB?: number;
  /** Where it comes from: 'built-in', 'cirrusstore' (official) or a third-party source. */
  source?: string;
  /** Its signed definition's serial. */
  serial?: number;
  /** Why it can't be installed (another source has its id). */
  unavailable?: string;
  /** It needs this CirrusUX version or newer: listed, but can't be installed yet. */
  needsVersion?: string;
  /** The oldest CirrusUX that can install it (from the definition). */
  minVersion?: string;
}

/** What the person sees when an app needs a newer CirrusUX. */
export const needsMessage = (version: string) => `This app needs CirrusUX ${version} or newer: run sudo cirrusux-setup upgrade on the server.`;

export const BUILT_IN = 'built-in';
export const OFFICIAL = 'cirrusstore';

/** An app's unique key in the merged catalogue. */
export const appKey = (a: { id: string; source?: string }) => `${a.source || BUILT_IN}/${a.id}`;

/** A source's name as the panel shows it. */
export const sourceLabel = (name?: string) => (!name || name === BUILT_IN ? 'Built in' : name === OFFICIAL ? 'CirrusStore' : name);

/** One source as the Sources card shows it (gateway.SourceStatus). */
export interface StoreSource {
  name: string;
  url: string;
  fingerprints: string[];
  official?: boolean;
  error?: string;
  status: 'ok' | 'not-checked' | 'expired' | 'unreachable' | 'refused' | 'unusable';
  detail?: string;
  checked?: string;
  apps: number;
}

/** The owner's choices on an app's page (catalogue.Options). */
export interface AppOptions {
  ports?: Record<string, number>;
  media?: Record<string, string>;
  env?: Record<string, string>;
  devices?: Record<string, boolean>;
}

/** The options a page starts with: suggested ports, defaults, and a folder
 *  for each media volume under the first media root. */
export function defaultOptions(app: CatalogueApp, mediaRoot = '/srv/media'): AppOptions {
  const o: AppOptions = { ports: {}, media: {}, env: {}, devices: {} };
  for (const p of app.ports) o.ports![p.name] = p.host;
  for (const v of app.volumes) if (v.kind === 'media') o.media![v.name] = `${mediaRoot.replace(/\/$/, '')}/${v.name}`;
  for (const e of app.env ?? []) if (e.choices) o.env![e.name] = e.default ?? e.choices[0];
  for (const d of app.devices ?? []) if (d.optional) o.devices![d.path] = false;
  return o;
}

/** Only what the server needs: unchanged defaults are left out. */
export function cleanOptions(app: CatalogueApp, o: AppOptions): AppOptions {
  const out: AppOptions = {};
  const ports = Object.fromEntries(Object.entries(o.ports ?? {}).filter(([k, v]) => app.ports.find((p) => p.name === k)?.host !== v));
  if (Object.keys(ports).length) out.ports = ports;
  if (Object.keys(o.media ?? {}).length) out.media = o.media;
  const env = Object.fromEntries(Object.entries(o.env ?? {}).filter(([k, v]) => app.env?.find((e) => e.name === k)?.default !== v));
  if (Object.keys(env).length) out.env = env;
  const devices = Object.fromEntries(Object.entries(o.devices ?? {}).filter(([, on]) => on));
  if (Object.keys(devices).length) out.devices = devices;
  return out;
}
