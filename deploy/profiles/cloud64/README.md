# cloud64 profile

The owner's own setup: cloud64 (a media server that sleeps) and cloud-box
(the always-on gateway, without systemd). The reinstall is in `INSTALL.md`;
the settings to fill in on each machine are the `*.env.example` files here,
with names and comments only.

`cirrusux-wake` and its sudoers rule are the old wake wrapper, kept until
`cirrusux-router-wake` (deploy/gateway) has woken cloud64 for real.

This folder is private. It moves out of the repository before the repo is
made public (ROADMAP Phase 7). No secrets, logins, addresses or device names
here: the real values live only on the machines.
