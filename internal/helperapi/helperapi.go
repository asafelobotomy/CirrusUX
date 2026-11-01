// Package helperapi is the contract between the agent (unprivileged) and
// cirrusux-helper (root, via sudo). Every subcommand is fixed and takes no
// caller-supplied paths; the helper prints one JSON value on stdout.
package helperapi

// Read-only subcommands (Phase 1).
const (
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
	CmdBackupRun    = "backup-run"    // start cloud64-backup.service, don't wait
	CmdEgressReport = "egress-report" // read-only, but slow: on demand only
)

var Commands = []string{
	CmdIdleStatus, CmdIdleLog, CmdBackupList, CmdBackupLast, CmdAVLast, CmdSecurityStatus,
	CmdStayAwake, CmdAllowSleep, CmdNudgeAwake, CmdBackupRun, CmdEgressReport,
}

// StayAwakeArgs are the only holds the panel can set.
var StayAwakeArgs = []string{"30", "120", "forever"}

// Result is what the Phase 2 subcommands print.
type Result struct {
	Output []string `json:"output"`
}

type IdleStatus struct {
	// Files in /run/cloud64-idle/, name → content (trimmed, size-limited).
	// The checker rewrites them every minute: last-msg (its verdict),
	// last-active, last-wake, last-suspend (unix seconds). The panel never
	// runs cloud64-idle-check itself: its --dry-run is not write-free.
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
	Banned    int      `json:"banned"`
	Lockdown  bool     `json:"lockdown"`
	PanelDrop bool     `json:"panelDrop"`
	Errors    []string `json:"errors,omitempty"`
}
