// The browser side of the terminal: xterm.js, loaded only when a session
// opens, talking to the gateway's /api/terminal. Output streams in on one
// long response; keys go out in order, one request at a time.
import { ApiError, api } from './api.svelte';

export interface Session {
  /** Ends the session; quietly when leaving the page. */
  close(leaving?: boolean): void;
  dispose(): void;
}

const H = { 'X-Cirrus-Request': '1' };

export async function openSession(el: HTMLElement, opts: { user: string; kind: 'ssh' | 'sftp' }, onEnd: () => void): Promise<Session> {
  const [{ Terminal }, { FitAddon }] = await Promise.all([import('@xterm/xterm'), import('@xterm/addon-fit'), import('@xterm/xterm/css/xterm.css')]);
  el.replaceChildren();
  const css = getComputedStyle(document.documentElement);
  const term = new Terminal({
    cursorBlink: true, fontSize: 13, scrollback: 5000, convertEol: false,
    fontFamily: css.getPropertyValue('--font-mono').trim() || 'monospace',
    theme: { background: '#0b0d12', foreground: '#e6e8ee', cursor: css.getPropertyValue('--accent').trim() || '#7aa2f7' },
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(el);
  fit.fit();

  const { id } = await api<{ id: string }>('/api/terminal', {
    method: 'POST', body: JSON.stringify({ ...opts, cols: term.cols, rows: term.rows }),
  }).catch((e) => {
    term.dispose();
    throw e instanceof ApiError ? new Error(e.message) : e;
  });
  const base = `/api/terminal/${id}`;
  let ended = false;
  const ctl = new AbortController();

  // Keys in order: each waits for the one before.
  let chain = Promise.resolve();
  const enc = new TextEncoder();
  const send = (data: string | Uint8Array) => {
    const body = typeof data === 'string' ? enc.encode(data) : data;
    chain = chain.then(() => fetch(`${base}/input`, { method: 'POST', headers: H, body: body as BodyInit }).then(() => undefined, () => undefined));
  };
  term.onData((d) => send(d));
  term.onBinary((d) => send(Uint8Array.from(d, (c) => c.charCodeAt(0))));
  term.onResize(({ cols, rows }) => {
    if (!ended) void fetch(`${base}/resize`, { method: 'POST', headers: { ...H, 'Content-Type': 'application/json' }, body: JSON.stringify({ cols, rows }) });
  });
  const ro = new ResizeObserver(() => fit.fit());
  ro.observe(el);
  term.focus();

  const finish = (msg: string) => {
    if (ended) return;
    ended = true;
    ro.disconnect();
    term.write(`\r\n\x1b[2m[${msg}]\x1b[0m\r\n`);
    onEnd();
  };

  void (async () => {
    try {
      const res = await fetch(`${base}/output`, { headers: H, signal: ctl.signal, cache: 'no-store' });
      if (!res.ok || !res.body) throw new Error((await res.text()).trim() || res.statusText);
      const reader = res.body.getReader();
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        term.write(value);
      }
      finish('session ended');
    } catch (e) {
      finish(ctl.signal.aborted ? 'disconnected' : `connection lost: ${(e as Error).message}`);
    }
  })();

  return {
    close(leaving = false) {
      if (!ended) void fetch(base, { method: 'DELETE', headers: H, keepalive: true });
      if (leaving) {
        ctl.abort();
        term.dispose();
      }
    },
    dispose() {
      ctl.abort();
      ro.disconnect();
      term.dispose();
    },
  };
}
