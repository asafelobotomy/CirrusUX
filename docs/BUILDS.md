# Builds

CirrusUX can build and test the repositories it keeps (see
[GIT.md](GIT.md)). A repository says what to run in `.cirrus/build.yml`;
you pick a branch or tag on its **Builds** tab and press **Run build**. The
server runs each step in a fresh container, and the panel shows each step's
result and the whole log as it runs.

It is off until you turn it on, and nothing runs by itself: every build is
one you start, confirm, and find in the Changelog. Running builds when you
push is planned (ROADMAP.md).

## Turning it on

Builds need Git hosting on the gateway, and the app store's container
runtime on the server (`CONTAINER_RUNTIME`, Docker or Podman), plus
nftables (`nft`) for the firewall rules below. In the server's
`/etc/cirrusux/helper.env`:

```sh
BUILDS=yes
# Optional, with their defaults:
#BUILD_MEMORY_MB=2048     # each step's memory, swap included
#BUILD_CPUS=2             # each step's CPUs
#BUILD_MAX_MINUTES=60     # the longest timeout a pipeline may ask for
#BUILD_SUBNET=10.231.0.0/24
```

The agent runs the helper's `build-job` and `build-cancel` through sudo.
Installing or upgrading with `cirrusux-setup` puts those lines in
`/etc/sudoers.d/cirrusux`; on a server set up by hand, copy them from
`deploy/server/sudoers-cirrusux`. Restart `cirrusux-agent`, and the Builds
tab appears once the server's next report says it can build.

## The pipeline file

```yaml
# .cirrus/build.yml
timeout: 20m            # optional; 30m if left out
steps:
  - name: test
    image: golang:1.25
    env:
      CGO_ENABLED: "0"
    commands:
      - go vet ./...
      - go test ./...
  - name: web
    image: node:24
    commands:
      - |
        cd web
        npm ci
        npm test
```

- **steps** run in order, up to 20. A step that fails stops the build; the
  steps after it are skipped.
- **name**: lower-case letters, digits, `.`, `_` and `-`.
- **image**: any image with `/bin/sh`, by tag or digest. Pin a digest
  (`node:24@sha256:…`) to be sure what runs.
- **commands**: run one after another with `sh -e`, each shown in the log
  before it runs. A `|` block is one command over several lines.
- **env**: plain variables. `HOME`, `CI`, `PATH` and names starting with
  `CIRRUS_` are CirrusUX's. Don't put secrets in the file: anyone who can
  read the repository can read them.

The file is a small part of YAML: mappings, lists, plain or quoted text
and `|` blocks. Anything else (`[a, b]` lists, anchors, `>` blocks) is
refused with the line number, as is any setting CirrusUX doesn't know,
`privileged:` and `services:` among them.

Each step gets `CI=true`, `CIRRUS_REPO`, `CIRRUS_REF`, `CIRRUS_COMMIT`,
`CIRRUS_STEP` and `CIRRUS_BUILD`.

## What a step can and can't do

Every step's container runs with settings the pipeline can't change:

| | |
|---|---|
| User | 65532, not root, with no capabilities and `no-new-privileges` |
| Files | The source at `/workspace`, which later steps see too, and `$HOME` (`/workspace/.cirrus-home`) for caches. The image's own files are read-only; `/tmp` is a 1 GB scratch space |
| Network | Its own network, `cirrusux-build`: the internet, but not this machine, your local network, Tailscale addresses or cloud metadata. DNS is allowed through |
| Limits | `BUILD_MEMORY_MB`, `BUILD_CPUS`, 1024 processes, and the pipeline's timeout |
| Never | Privileged mode, the host's network, the Docker or Podman socket, devices, other folders |

So a step can download packages and run tests, but it can't install
system packages as root (use an image that has what you need), and it
can't build container images, since that needs privileges or the
runtime's socket. That may come later, with a rootless builder, if it can
be done safely.

The firewall rules are an nftables table, `inet cirrusux_build`, loaded
before every build; if they can't be loaded, the build stops before
anything runs. Builds use the same container runtime as your apps, but
their own network, and each container is removed after its step.

The source is the commit as `git archive` makes it: no `.git` folder, and
no submodules.

## While it runs

The server keeps the build's job unit (`cirrusux-job-<id>`) running, which
also keeps a sleeping server awake until it ends. **Cancel** stops the step
that is running and skips the rest. A build that is still waiting to start
is cancelled on the gateway.

In the split setup the repositories are on the gateway and builds run on
the server, so the server must be awake to start one: wake it first.

The panel keeps the first 4 MB of a build's log, and the last 30 builds of
each repository. The server keeps each build's status and log for a week in
`/var/lib/cirrusux/builds/`, and removes the workspace when the build
ends. A repository that is renamed keeps its old builds under the old
name.

## Limits

- One step at a time per build; you can start several builds, and they
  run side by side.
- The source is up to 1 GB packed and 2 GB unpacked.
- The workspace has no size limit of its own: it uses the server's disk.
- A pipeline file is up to 64 KB, with up to 50 commands a step.
