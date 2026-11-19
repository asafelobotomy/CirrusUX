// Package helperapi is the contract between the agent (unprivileged) and
// cirrusux-helper (root, via sudo). Every subcommand is fixed. The only
// things the caller supplies are a job ID, a unit name, an archive entry's
// name, a stay-awake length and a release tag, each matched against a fixed pattern in
// the helper; paths come from root's own config file. The helper prints one
// JSON value on stdout.
package helperapi

import (
	"regexp"
	"time"

	"cirrusux/internal/store"
)

// Read-only subcommands (Phase 1).
const (
	CmdFeatures       = "features"  // which modules this machine has
	CmdAppsInfo       = "apps-info" // the app store's settings, for previews
	CmdIdleStatus     = "idle-status"
	CmdIdleLog        = "idle-log"
	CmdBackupList     = "backup-list"
	CmdBackupLast     = "backup-last"
	CmdAVLast         = "av-last"
	CmdSecurityStatus = "security-status"
)

// Phase 2 subcommands that change state. Each takes a fixed argument list,
// so the sudoers file names every allowed command line in full.
const (
	CmdStayAwake    = "stay-awake"    // 30 | 120 | forever
	CmdAllowSleep   = "allow-sleep"   // remove the hold
	CmdNudgeAwake   = "nudge-awake"   // ≥10 min, unless a longer hold exists (decision D3)
	CmdBackupRun    = "backup-run"    // start the backup unit, don't wait
	CmdEgressReport = "egress-report" // read-only, but slow: on demand only
	// CmdAppJob <job> takes the request the agent left in RequestsDir as
	// <job>.json, checks it against the built-in catalogue and starts it.
	CmdAppJob = "app-job"
	// CmdAppRun runs a checked job; systemd starts it, never sudo.
	CmdAppRun = "app-run"
	// OS updates, only when asked: each starts a background job.
	CmdOSCheck   = "os-check"
	CmdOSUpgrade = "os-upgrade"
	CmdOSRun     = "os-run" // the job itself; systemd starts it, never sudo
	// CmdJournal prints the recent journal of one CirrusUX unit.
	CmdJournal = "journal"
	// Storage (Phase 6c): measuring is a job; deleting takes the exact
	// name of an archive entry and is the only deletion in CirrusUX.
	CmdStorageScan   = "storage-scan"
	CmdArchiveDelete = "archive-delete"
	CmdStorageRun    = "storage-run" // the jobs themselves; never sudo
	// CirrusUX's own release: upgrade takes the release tag (vX.Y.Z),
	// rollback goes back to the one before. Each starts a background job
	// that runs `cirrusux-setup upgrade` or `rollback`.
	CmdSelfUpgrade  = "self-upgrade"
	CmdSelfRollback = "self-rollback"
	CmdSelfRun      = "self-run" // the job itself; never sudo
	// Accounts and SSH: reading is CmdAccessStatus; the changes take a
	// login name and a key (base64url) or a key's SHA256 fingerprint.
	CmdAccessStatus  = "access-status"
	CmdSSHKeyAdd     = "ssh-key-add"
	CmdSSHKeyRemove  = "ssh-key-remove"
	CmdAccountLock   = "account-lock"
	CmdAccountUnlock = "account-unlock"
	// Creating and removing login accounts the panel made (access module).
	CmdAccountCreate = "account-create" // <name> user|admin
	CmdAccountRemove = "account-remove" // <name>; keeps the home folder
	// Network settings (network-edit module): DNS servers and Tailscale's
	// preferences, each undone by the server after NetRevertSeconds unless
	// kept. net-status reads; net-rollback is the timer's, never sudo.
	CmdNetStatus    = "net-status"
	CmdNetDNS       = "net-dns"       // <servers|-> <search|->
	CmdNetTailscale = "net-tailscale" // yes|no (accept DNS) yes|no (accept routes)
	CmdNetConfirm   = "net-confirm"
	CmdNetRevert    = "net-revert"
	CmdNetRollback  = "net-rollback"
	// CmdKeysIO reads or writes one account's authorized_keys, run by the
	// helper as that account; never sudo.
	CmdKeysIO = "keys-io"
	// Builds (docs/BUILDS.md): build-job <job> takes the request and source
	// the agent left in RequestsDir and starts the build; build-cancel
	// stops one. build-run is the build itself; systemd starts it, never sudo.
	CmdBuildJob    = "build-job"
	CmdBuildCancel = "build-cancel"
	CmdBuildRun    = "build-run"
)

var keyArgRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidKeyArg checks a public key line as the agent passes it: base64url,
// no padding, 16 to 4000 characters.
func ValidKeyArg(s string) bool { return len(s) >= 16 && len(s) <= 4000 && keyArgRe.MatchString(s) }

var Commands = []string{
	CmdFeatures, CmdAppsInfo, CmdIdleStatus, CmdIdleLog, CmdBackupList, CmdBackupLast, CmdAVLast, CmdSecurityStatus,
	CmdStayAwake, CmdAllowSleep, CmdNudgeAwake, CmdBackupRun, CmdEgressReport, CmdAppJob,
	CmdOSCheck, CmdOSUpgrade, CmdJournal, CmdStorageScan, CmdArchiveDelete,
	CmdSelfUpgrade, CmdSelfRollback, CmdBuildJob, CmdBuildCancel,
	CmdAccessStatus, CmdSSHKeyAdd, CmdSSHKeyRemove, CmdAccountLock, CmdAccountUnlock, CmdAccountCreate, CmdAccountRemove,
	CmdNetStatus, CmdNetDNS, CmdNetTailscale, CmdNetConfirm, CmdNetRevert,
}

// GatewayCommands are the only ones the gateway's machine allows in a split
// setup, where the helper is installed just to upgrade CirrusUX there.
var GatewayCommands = []string{CmdSelfUpgrade, CmdSelfRollback}

// StorageFile holds the last measurement and the archive list.
const StorageFile = "/var/lib/cirrusux/storage.json"

// OSUpdatesFile holds the last OS update check, readable by the agent.
const OSUpdatesFile = "/var/lib/cirrusux/os-updates.json"

// RebootRequiredFile is Debian's sign that an upgrade needs a restart; a
// restart clears it (it's in /run).
const RebootRequiredFile = "/run/reboot-required"

// App store paths shared by the agent and the helper.
const (
	// RequestsDir is the agent's own folder for job requests
	// (RuntimeDirectory=cirrusux-agent); the helper reads and removes them.
	RequestsDir = "/run/cirrusux-agent/requests"
	// JobsDir holds job statuses, readable by the agent.
	JobsDir = "/run/cirrusux-jobs"
	// AppRecordsDir holds installed apps' records, readable by the agent.
	AppRecordsDir = "/var/lib/cirrusux/apps"
	// BuildsDir holds a folder per build: its status and log, readable by
	// the agent, and its workspace while it runs.
	BuildsDir = "/var/lib/cirrusux/builds"
)

// StayAwakeArgs are the only holds the panel can set.
var StayAwakeArgs = []string{"30", "120", "forever"}

// Modules a machine can have, set up in the helper's config file
// (/etc/cirrusux/helper.env). Containers need none of them.
const (
	ModSleep     = "sleep"        // an idle checker that suspends the server
	ModStayAwake = "stay-awake"   // holds that keep it awake
	ModBackup    = "backup"       // a systemd unit that makes system backups
	ModAV        = "av"           // a ClamAV scan log
	ModSecurity  = "security"     // fail2ban jail and firewall checks
	ModEgress    = "egress"       // the outbound-traffic report
	ModApps      = "apps"         // the app store: a container runtime and an apps folder
	ModOS        = "os"           // Debian package updates, on request
	ModStorage   = "storage"      // folder sizes and the archive
	ModAccess    = "access"       // login accounts, SSH keys, SFTP and FTPS
	ModNetEdit   = "network-edit" // DNS servers and Tailscale preferences, with automatic rollback
	ModBuilds    = "builds"       // runs repositories' build pipelines in containers
)

// Features is what `features` prints.
type Features struct {
	Modules []string `json:"modules"`
}

// AppsInfo is what an install will use on this server: the same facts the
// helper's jobs resolve apps with, so the panel's preview matches.
type AppsInfo struct {
	Runtime    string         `json:"runtime"`
	AppsDir    string         `json:"appsDir"`
	MediaRoots []string       `json:"mediaRoots"`
	Bind       string         `json:"bind,omitempty"`
	TZ         string         `json:"tz"`
	UID        int            `json:"uid"`
	GID        int            `json:"gid"`
	Groups     map[string]int `json:"groups"`
	// Sources are the CirrusStore catalogues the helper trusts, with their
	// keys: the gateway checks what it fetches against them, and the
	// helper checks every install again.
	Sources []store.Info `json:"sources"`
}

// BackupLast is the backup unit's recent journal.
type BackupLast struct {
	Unit  string   `json:"unit"`
	Lines []string `json:"lines"`
}

// Result is what the Phase 2 subcommands print.
type Result struct {
	Output []string `json:"output"`
}

type IdleStatus struct {
	// Files in the idle checker's state directory, name → content (trimmed, size-limited).
	// The checker rewrites them every minute: last-msg (its verdict),
	// last-active, last-wake, last-suspend (unix seconds). The panel never
	// runs the idle checker itself, so reading never changes its state.
	State map[string]string `json:"state"`
	// Content of /run/stay-awake, nil when the file does not exist.
	StayAwake *string `json:"stayAwake"`
}

type Lines struct {
	Lines []string `json:"lines"`
}

type BackupFile struct {
	Name  string `json:"name"`
	MTime int64  `json:"mtime"` // unix seconds
	Bytes int64  `json:"bytes"`
}

type BackupList struct {
	Files []BackupFile `json:"files"`
}

type SecurityStatus struct {
	Fail2ban bool            `json:"fail2ban"` // a jail is set up
	Banned   int             `json:"banned"`
	Firewall []FirewallCheck `json:"firewall"`
	Errors   []string        `json:"errors,omitempty"`
}

// FirewallCheck is one configured rule check: Chain holds the pattern.
type FirewallCheck struct {
	Chain string `json:"chain"`
	OK    bool   `json:"ok"`
}

// UnitRe matches CirrusUX's own services, whose journals the panel may read.
var UnitRe = regexp.MustCompile(`^cirrusux-[a-z0-9-]{1,40}\.service$`)

// JobRe matches a job's ID: 8 to 32 hex digits.
var JobRe = regexp.MustCompile(`^[0-9a-f]{8,32}$`)

// Network settings that can be undone by the server itself.
const (
	// NetPendingFile holds the change waiting to be kept, readable by the agent.
	NetPendingFile = "/var/lib/cirrusux/net-pending.json"
	// NetRevertSeconds is how long a change waits to be kept.
	NetRevertSeconds = 120
	// NetRevertUnit is the timer that undoes it.
	NetRevertUnit = "cirrusux-job-netrevert"
)

// TSPrefs are the Tailscale preferences the panel changes.
type TSPrefs struct {
	AcceptDNS    bool `json:"acceptDns"`
	AcceptRoutes bool `json:"acceptRoutes"`
}

// NetPending is a change the server will undo at Deadline unless it's kept.
type NetPending struct {
	Kind     string    `json:"kind"` // dns or tailscale
	What     string    `json:"what"`
	Deadline time.Time `json:"deadline"`
	// What it was before: the drop-in's text (DNSHad says there was one), or
	// Tailscale's preferences.
	DNSHad  bool     `json:"dnsHad,omitempty"`
	DNSPrev string   `json:"dnsPrev,omitempty"`
	TSPrev  *TSPrefs `json:"tsPrev,omitempty"`
}

// NetStatus is what net-status prints.
type NetStatus struct {
	// Resolved: systemd-resolved is running, so DNS can be set here.
	Resolved bool `json:"resolved"`
	// DNS and Search are what the panel set (empty: the system's own).
	DNS    []string `json:"dns"`
	Search []string `json:"search"`
	// Tailscale is nil when Tailscale isn't installed or running.
	Tailscale *TSPrefs    `json:"tailscale,omitempty"`
	Pending   *NetPending `json:"pending,omitempty"`
}
