// Demo-mode CirrusStore: the built-in apps, a newer Navidrome definition
// from the official source, and one third-party source's apps, including
// one whose id is taken. The keys and fingerprints are made up.
import demoCatalogue from 'virtual:catalogue';
import { BUILT_IN, OFFICIAL, type CatalogueApp, type StoreSource } from '../catalogue';

export function demoStoreCatalogue(): CatalogueApp[] {
  const apps: CatalogueApp[] = demoCatalogue.map((a) =>
    a.id === 'navidrome' ? { ...a, source: OFFICIAL, serial: 2, notes: 'Newer definition from CirrusStore: scans the library every hour.' } : { ...a, source: BUILT_IN },
  );
  const navidrome = demoCatalogue.find((a) => a.id === 'navidrome');
  if (navidrome) {
    apps.push({
      ...navidrome,
      id: 'music-box',
      name: 'Music Box',
      description: 'A music server from a friend’s catalogue: the same as Navidrome, on another port.',
      ports: navidrome.ports.map((p) => ({ ...p, host: p.host + 100 })),
      source: 'friends',
      serial: 3,
    });
    apps.push({ ...navidrome, source: 'friends', serial: 1, unavailable: 'CirrusStore already has an app with the id "navidrome"' });
  }
  return apps;
}

export function demoSources(fetched: Date): StoreSource[] {
  const checked = fetched.toISOString();
  return [
    {
      name: OFFICIAL,
      url: 'https://asafelobotomy.github.io/cirrusstore/',
      fingerprints: ['SHA256:demo0fficia1keyXq2v9Yb3kLm8Rt4Wn6Ps1Zc5Hd7Jf0'],
      official: true,
      status: 'ok',
      checked,
      apps: 1,
    },
    {
      name: 'friends',
      url: 'https://apps.example.org/catalogue/',
      fingerprints: ['SHA256:demoFr1endsKeyA8s7d6f5g4h3j2k1l0p9o8i7u6y5t4r'],
      status: 'ok',
      checked,
      apps: 2,
    },
  ];
}
