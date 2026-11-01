// Package helperapi is the contract between the agent (unprivileged) and
// c64panel-helper (root, via sudo). Every subcommand is fixed and takes no
// caller-supplied paths; the helper prints one JSON value on stdout.
package helperapi

// Subcommands the sudoers entry allows. Phase 1 is read-only.
const (
	CmdIdleStatus     = "idle-status"
	CmdIdleLog        = "idle-log"
	CmdBackupList     = "backup-list"
	CmdBackupLast     = "backup-last"
	CmdAVLast         = "av-last"
	CmdSecurityStatus = "security-status"
)

var Commands = []string{CmdIdleStatus, CmdIdleLog, CmdBackupList, CmdBackupLast, CmdAVLast, CmdSecurityStatus}

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
