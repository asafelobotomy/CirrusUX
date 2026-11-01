# CirrusUX — web UI

With a gateway answering on `/api` the UI shows cloud64's real state (live
mode); without one it runs on sample data from the 1 Nov 2026 brief (demo
mode, marked "Demo data"). See `../docs/PLAN.md` §4 for the design spec.

```bash
npm ci
npm run dev      # http://127.0.0.1:5174
npm run check    # svelte-check, must be 0 errors / 0 warnings
npm run build    # static files in dist/
```

## What to try

- **Prototype** button (bottom centre, demo mode only): switch cloud64 between awake, asleep
  and the wake sequence; simulate streaming activity; reset all settings.
- **Edit layout** on Home: drag the grip to reorder, the corner to resize,
  add widgets, add boards. Phone, tablet and desktop each keep a layout; the
  board picks one from its own width (opening Grok on a laptop switches to
  the tablet layout).
- **Ctrl/⌘ K**: command palette for pages, actions, themes, or "Ask Grok".
- **Grok** (sparkle button): canned answers, tool badges, proposal-only
  actions that go through the confirm dialog.
- **Settings**: themes, follow-system, accent with automatic contrast fix,
  density, text size, wallpapers, custom CSS, export/import.
- **Theme editor**: edit every token with live preview and WCAG checks.
- **Components**: every component, plus the theme matrix.

## Layout

```
src/
  App.svelte                 shell, routing, theme application
  pages/                     Home, Services, Backups, Changelog, Settings,
                             ThemeEditor, Gallery, Placeholder
  lib/styles/base.css        derived tokens, base element styles
  lib/theme/                 built-in themes, contrast maths
  lib/state/                 settings, power, layout, actions, grok, ui
  lib/components/            Button, Badge, Dialog, Toggle, Segmented, …
  lib/widgets/               dashboard widgets + registry
  lib/layout/                Board (grid, drag, resize), WidgetPicker
  lib/shell/                 Sidebar, TopBar, BottomBar, palette, Grok panel
  lib/fake/                  sample data and the stand-in settings store
```

Demo mode only:

- Settings and layouts persist in this browser (`lib/fake/store.ts`); live
  mode stores them on the gateway.
- Power state, actions and Grok answers are simulated.
- The clock starts at 1 Nov 2026 20:40 GMT so the sample data lines up.
