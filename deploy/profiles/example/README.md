# Example profile

A profile is a folder of settings for one particular server: the
`*.env.example` files filled in with that server's names, its wake method and
its allowed logins. It is not part of CirrusUX itself, so real profiles are
kept out of the repository (`.gitignore` skips everything in `deploy/profiles/`
except this example).

To make one for your own server:

1. Copy `deploy/server/*.env.example` and `deploy/gateway/*.env.example` into a
   new folder here, for example `deploy/profiles/homeserver/`.
2. Fill in names and comments only: `homeserver`, `192.0.2.10`,
   `aa:bb:cc:dd:ee:ff`. Never put passwords, tokens or real addresses in a
   file you might commit. Real values live on the machines, in
   `/etc/cirrusux/*.env`.
3. Keep notes for your own install steps next to them.

See `docs/INSTALL.md` for the steps and `docs/CONFIG.md` for every setting.
