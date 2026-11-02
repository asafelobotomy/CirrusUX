import { test } from 'node:test';
import assert from 'node:assert/strict';
import { appName, tilesFor } from '../src/lib/apps.ts';

test('container names map to app names', () => {
  const cases: Record<string, string> = {
    '1Panel-jellyfin-a1B2': 'Jellyfin',
    navidrome: 'Navidrome',
    kavita: 'Kavita',
    tinymediamanager: 'tinyMediaManager',
    'musicbrainz-picard-1': 'Picard',
    nginx_server: 'Nginx_server',
    'cirrus-test': 'Cirrus-test',
    'home-assistant': 'Home Assistant',
  };
  for (const [container, app] of Object.entries(cases)) assert.equal(appName(container), app, container);
});

test('tiles come from containers that publish a port', () => {
  const tiles = tilesFor([
    { name: 'jellyfin', port: 8096 },
    { name: 'musicbrainz-picard-1', port: 5800 },
    { name: 'nginx_server', port: null },
    { name: 'whoami', port: 8000 },
  ]);
  assert.deepEqual(tiles.map((t) => t.name), ['Jellyfin', 'Picard', 'Whoami']);
  assert.equal(tiles[1].https, true);
  assert.equal(tiles[2].icon, 'Package');
  assert.equal(tiles[2].group, 'Apps');
  assert.match(tiles[2].color, /^#[0-9a-f]{6}$/);
});
