// Package helperapi is the contract between the agent (unprivileged) and
// cirrusux-helper (root, via sudo). Every subcommand is fixed and takes no
// caller-supplied paths; the helper prints one JSON value on stdout.
package helperapi

import "regexp"

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
)

var Commands = []string{
	CmdFeatures, CmdAppsInfo, CmdIdleStatus, CmdIdleLog, CmdBackupList, CmdBackupLast, CmdAVLast, CmdSecurityStatus,
	CmdStayAwake, CmdAllowSleep, CmdNudgeAwake, CmdBackupRun, CmdEgressReport, CmdAppJob,
	CmdOSCheck, CmdOSUpgrade, CmdJournal, CmdStorageScan, CmdArchiveDelete,
}

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
)

// StayAwakeArgs are the only holds the panel can set.
var StayAwakeArgs = []string{"30", "120", "forever"}

// Modules a machine can have, set up in the helper's config file
// (/etc/cirrusux/helper.env). Containers need none of them.
const (
	ModSleep     = "sleep"      // an idle checker that suspends the server
	ModStayAwake = "stay-awake" // holds that keep it awake
	ModBackup    = "backup"     // a systemd unit that makes system backups
	ModAV        = "av"         // a ClamAV scan log
	ModSecurity  = "security"   // fail2ban jail and firewall checks
	ModEgress    = "egress"     // the outbound-traffic report
	ModApps      = "apps"       // the app store: a container runtime and an apps folder
	ModOS        = "os"         // Debian package updates, on request
	ModStorage   = "storage"    // folder sizes and the archive
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
