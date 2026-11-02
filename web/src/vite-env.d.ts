/// <reference types="vite/client" />
/// <reference types="svelte" />
declare module '@fontsource-variable/*';

/** The UI's version, from ../VERSION at build time. */
declare const __CIRRUSUX_VERSION__: string;

/** The app store's catalogue (internal/catalogue/apps), for demo mode. */
declare module 'virtual:catalogue' {
  const apps: import('./lib/catalogue').CatalogueApp[];
  export default apps;
}
