import { test } from 'node:test';
import assert from 'node:assert/strict';
import { appName, apps } from '../src/lib/apps.ts';

test('container names map to app names', () => {
  const cases: Record<string, string> = {
    '1Panel-jellyfin-a1B2': 'Jellyfin',
    '1Panel-navidrome-c3D4': 'Navidrome',
    kavita: 'Kavita',
    tinymediamanager: 'tinyMediaManager',
    'musicbrainz-picard-1': 'Picard',
    nginx_server: 'Nginx_server',
  };
  for (const [container, app] of Object.entries(cases)) assert.equal(appName(container), app, container);
});

test('every containerised app tile matches a container name', () => {
  const known = ['jellyfin', 'navidrome', 'audiobookshelf', 'kavita', 'tinymediamanager', 'musicbrainz-picard'].map(appName);
  for (const a of apps.filter((x) => x.inContainer)) assert.ok(known.includes(a.name), a.name);
});
