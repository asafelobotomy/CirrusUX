// Package snapshot defines what the agent on cloud64 reports to the gateway.
// The agent pushes one every few minutes while cloud64 is awake; the gateway
// caches the latest so the UI and Grok can answer while it sleeps.
package snapshot

import "time"

type Snapshot struct {
	Host       string      `json:"host"`
	TakenAt    time.Time   `json:"takenAt"`
	BootAt     time.Time   `json:"bootAt"`
	Power      Power       `json:"power"`
	Containers []Container `json:"containers"`
	Backups    Backups     `json:"backups"`
	Disk       *Disk       `json:"disk,omitempty"`
	// CPU temperature samples in °C, oldest first, one per sample interval.
	Temps    []float64 `json:"temps"`
	Security *Security `json:"security,omitempty"`
	Timeline []Segment `json:"timeline"`
	// Problems collecting individual sections; the rest of the snapshot is
	// still valid.
	Errors []string `json:"errors,omitempty"`
}

type Power struct {
	// Last resume from sleep, or boot.
	Since        time.Time  `json:"since"`
	LastActivity *time.Time `json:"lastActivity,omitempty"`
	IdleMin      int        `json:"idleMin"`
	GraceMin     int        `json:"graceMin"`
	// Set when /run/stay-awake exists. Forever when it has no expiry.
	StayAwakeUntil   *time.Time `json:"stayAwakeUntil,omitempty"`
	StayAwakeForever bool       `json:"stayAwakeForever,omitempty"`
	// What the idle check currently counts as activity.
	Reasons   []string `json:"reasons"`
	NextWakes []Wake   `json:"nextWakes"`
}

type Wake struct {
	At   time.Time `json:"at"`
	Unit string    `json:"unit"`
}

type Container struct {
	Name    string     `json:"name"`
	Image   string     `json:"image"`
	Tag     string     `json:"tag"`
	State   string     `json:"state"`  // running, exited, …
	Health  string     `json:"health"` // healthy, unhealthy, starting, running, exited
	Status  string     `json:"status"` // Docker's own text, e.g. "Up 20 minutes (healthy)"
	Ports   []int      `json:"ports"`
	UpSince *time.Time `json:"upSince,omitempty"`
}

type Backups struct {
	Files   []BackupFile `json:"files"`
	LastRun *BackupRun   `json:"lastRun,omitempty"`
}

type BackupFile struct {
	Name      string    `json:"name"`
	At        time.Time `json:"at"`
	Bytes     int64     `json:"bytes"`
	Encrypted bool      `json:"encrypted"`
}

type BackupRun struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	OK    bool      `json:"ok"`
	// Last relevant journal line, e.g. "OK 2.0G".
	Summary string `json:"summary"`
}

type Disk struct {
	Mount      string `json:"mount"`
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	// USB link speed in Mb/s when the disk sits behind USB, else 0.
	LinkMbps int `json:"linkMbps"`
}

type Security struct {
	ClamAV    *ClamAV `json:"clamav,omitempty"`
	Banned    int     `json:"banned"`
	Lockdown  bool    `json:"lockdown"`
	PanelDrop bool    `json:"panelDrop"`
}

type ClamAV struct {
	At       time.Time `json:"at"`
	Files    int       `json:"files"`
	Infected int       `json:"infected"`
}

type Segment struct {
	From   time.Time  `json:"from"`
	To     *time.Time `json:"to,omitempty"` // nil while ongoing
	State  string     `json:"state"`        // awake, asleep
	Reason string     `json:"reason,omitempty"`
}
