package build

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// How a step's container runs. None of this comes from the pipeline: a
// pipeline names an image and commands, and the rest is fixed here.
const (
	// Network is the container network every build step joins: not the
	// host's, and kept off the local network by Firewall.
	Network = "cirrusux-build"
	// Bridge is that network's interface on the host, which the firewall
	// rules match.
	Bridge = "cirrusbuild0"
	// DefaultSubnet is the network's addresses unless BUILD_SUBNET says
	// otherwise.
	DefaultSubnet = "10.231.0.0/24"
	// UID runs every step: not root, and no user the host has.
	UID = 65532
	// Workspace is where the source is, inside each step.
	Workspace = "/workspace"
	// CacheMount is where a pipeline's cache folder is, in each step.
	CacheMount = "/cache"
	// Home is the steps' home folder, inside the workspace so that caches
	// (Go's, npm's) carry on to later steps.
	Home = Workspace + "/.cirrus-home"
)

// Limits are the helper's own settings for each step's container.
type Limits struct {
	MemoryMB int
	CPUs     int
	Pids     int
}

// ContainerName is a step's container: one per step, removed after it.
func ContainerName(job string, step int) string {
	return "cirrusux-build-" + job + "-" + strconv.Itoa(step+1)
}

// RunArgs is the argument list for `docker run` or `podman run` (the same
// for both) for step i of a build, with the source at workspace on the
// host.
func RunArgs(r Request, i int, workspace, cache string, lim Limits) []string {
	s := r.Pipeline.Steps[i]
	mem := strconv.Itoa(lim.MemoryMB) + "m"
	args := []string{
		"run", "--rm", "--name", ContainerName(r.Job, i),
		"--network", Network,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--user", fmt.Sprintf("%d:%d", UID, UID),
		"--read-only",
		"--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=1g",
		"--memory", mem, "--memory-swap", mem,
		"--cpus", strconv.Itoa(lim.CPUs),
		"--pids-limit", strconv.Itoa(lim.Pids),
		"--mount", "type=bind,src=" + workspace + ",dst=" + Workspace,
		"--workdir", Workspace,
	}
	if cache != "" {
		args = append(args, "--mount", "type=bind,src="+cache+",dst="+CacheMount)
	}
	args = append(args,
		"--label", "cirrusux.build="+r.Job)
	env := map[string]string{
		"HOME": Home, "CI": "true",
		"CIRRUS_REPO": r.Repo, "CIRRUS_REF": r.Ref, "CIRRUS_COMMIT": r.Commit,
		"CIRRUS_STEP": s.Name, "CIRRUS_BUILD": r.Job,
	}
	if cache != "" {
		env["CIRRUS_CACHE"] = CacheMount
	}
	maps.Copy(env, s.Env) // Validate keeps the pipeline off CirrusUX's names
	for _, k := range slices.Sorted(maps.Keys(env)) {
		args = append(args, "--env", k+"="+env[k])
	}
	// A secret is named, not given: the runtime takes its value from its own
	// environment (SecretEnv), so it never shows in a process listing.
	for _, n := range slices.Sorted(slices.Values(s.Secrets)) {
		args = append(args, "--env", n)
	}
	return append(args, "--entrypoint", "/bin/sh", s.Image, "-c", Script(s.Commands))
}

// Script runs commands in order and stops at the first that fails, showing
// each before it runs.
func Script(cmds []string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	for _, c := range cmds {
		b.WriteString("printf '%s\\n' " + shellQuote("$ "+firstLine(c)) + "\n")
		b.WriteString(c + "\n")
	}
	return b.String()
}

func firstLine(s string) string {
	if l, _, more := strings.Cut(strings.TrimSpace(s), "\n"); more {
		return l + " …"
	}
	return strings.TrimSpace(s)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// NetworkArgs creates the build network for the runtime.
func NetworkArgs(runtime, subnet string) []string {
	if runtime == "podman" {
		return []string{"network", "create", "--subnet", subnet, "--interface-name", Bridge, Network}
	}
	return []string{"network", "create", "--driver", "bridge", "--subnet", subnet,
		"--opt", "com.docker.network.bridge.name=" + Bridge,
		"--opt", "com.docker.network.bridge.enable_icc=false", Network}
}

// localNets are the addresses a build may not reach: this machine, the
// local network, Tailscale and link-local (cloud metadata) addresses.
var localNets = []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "240.0.0.0/4"}

// Firewall is the nftables ruleset that keeps build containers off this
// machine and the local network; the internet stays open. It replaces
// itself whole each time (nft -f), so it can be loaded before every build.
// DNS is let through, since containers ask the runtime's resolver or the
// one the host uses, which is often on the local network. A drop in any
// chain is final in nftables, so the runtime's own rules can't undo it.
func Firewall() string {
	nets := strings.Join(localNets, ", ")
	return `table inet cirrusux_build {}
delete table inet cirrusux_build
table inet cirrusux_build {
	chain input {
		type filter hook input priority filter - 5; policy accept;
		iifname "` + Bridge + `" meta l4proto { tcp, udp } th dport 53 accept
		iifname "` + Bridge + `" drop
	}
	chain forward {
		type filter hook forward priority filter - 5; policy accept;
		iifname "` + Bridge + `" meta nfproto ipv6 drop
		iifname "` + Bridge + `" meta l4proto { tcp, udp } th dport 53 accept
		iifname "` + Bridge + `" ip daddr { ` + nets + ` } drop
	}
}
`
}

// SecretEnv is the environment the runtime is started with so that it can
// pass a step's secrets on by name.
func SecretEnv(r Request, step int) []string {
	var env []string
	for _, n := range slices.Sorted(slices.Values(r.Pipeline.Steps[step].Secrets)) {
		env = append(env, n+"="+r.Secrets[n])
	}
	return env
}

// CacheDir is a cache folder under root.
func CacheDir(root, repo, key string) string { return root + "/" + repo + "/" + key }
