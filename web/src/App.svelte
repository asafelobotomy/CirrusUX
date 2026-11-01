<script lang="ts">
  import Sidebar from './lib/shell/Sidebar.svelte';
  import TopBar from './lib/shell/TopBar.svelte';
  import BottomBar from './lib/shell/BottomBar.svelte';
  import CommandPalette from './lib/shell/CommandPalette.svelte';
  import GrokPanel from './lib/shell/GrokPanel.svelte';
  import ConfirmDialog from './lib/shell/ConfirmDialog.svelte';
  import Toasts from './lib/shell/Toasts.svelte';
  import Wallpaper from './lib/shell/Wallpaper.svelte';
  import DevToolbar from './lib/shell/DevToolbar.svelte';
  import WidgetPicker from './lib/layout/WidgetPicker.svelte';
  import Home from './pages/Home.svelte';
  import Services from './pages/Services.svelte';
  import Backups from './pages/Backups.svelte';
  import Changelog from './pages/Changelog.svelte';
  import Security from './pages/Security.svelte';
  import Settings from './pages/Settings.svelte';
  import ThemeEditor from './pages/ThemeEditor.svelte';
  import Gallery from './pages/Gallery.svelte';
  import Placeholder from './pages/Placeholder.svelte';
  import { ui } from './lib/state/ui.svelte';
  import { applyTheme, settings } from './lib/state/settings.svelte';
  import { mode } from './lib/api.svelte';

  let pickerOpen = $state(false);

  // Re-applies whenever any theme input changes (theme, accent, draft, density…).
  $effect(applyTheme);

  // Custom CSS (PLAN §4.5 level 4) goes in its own style element.
  $effect(() => {
    let el = document.getElementById('custom-css');
    if (!el) {
      el = document.createElement('style');
      el.id = 'custom-css';
      document.head.append(el);
    }
    el.textContent = settings.customCss;
  });

  $effect(() => {
    if (ui.route !== 'home') ui.editLayout = false;
  });

  const PAGES = { services: Services, backups: Backups, changelog: Changelog, security: Security, settings: Settings, 'theme-editor': ThemeEditor, gallery: Gallery };
  const Page = $derived(PAGES[ui.route as keyof typeof PAGES] ?? Placeholder);
</script>

<Wallpaper />
<a class="skip" href="#main">Skip to content</a>

<div class="app" class:phone={ui.device === 'phone'} class:grok={ui.grokOpen}>
  {#if ui.device !== 'phone'}<Sidebar collapsed={ui.device === 'tablet'} />{/if}
  <div class="col">
    <TopBar onAddWidget={() => (pickerOpen = true)} />
    <main id="main">
      {#key ui.route}
        <div class="page">
          {#if ui.route === 'home'}<Home onAddWidget={() => (pickerOpen = true)} />{:else}<Page />{/if}
        </div>
      {/key}
    </main>
  </div>
</div>

{#if ui.device === 'phone'}<BottomBar />{/if}
<GrokPanel />
<CommandPalette />
<ConfirmDialog />
<WidgetPicker bind:open={pickerOpen} />
<Toasts />
{#if !mode.live}<DevToolbar />{/if}

<style>
  .app { display: flex; min-height: 100dvh; transition: padding-right var(--dur-2) var(--ease); }
  @media (min-width: 1100px) { .app.grok { padding-right: 420px; } }
  .col { flex: 1; min-width: 0; display: flex; flex-direction: column; }
  main { flex: 1; padding: var(--space-5) var(--space-6) var(--space-6); max-width: 1680px; width: 100%; margin: 0 auto; }
  .phone main { padding: var(--space-4) var(--space-4) calc(110px + env(safe-area-inset-bottom)); }
  .page { animation: enter var(--dur-3) var(--ease); }
  @keyframes enter { from { opacity: 0; transform: translateY(6px); } }
  .skip { position: absolute; left: -9999px; top: 8px; z-index: 100; padding: 8px 12px; background: var(--accent); color: var(--on-accent); border-radius: var(--radius-sm); }
  .skip:focus { left: 8px; }
</style>
