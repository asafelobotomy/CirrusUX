// Package snapshot defines what the agent reports to the gateway. The agent
// pushes one every few minutes while the server is awake; the gateway
// caches the latest so the UI and the assistant can answer while it sleeps.
package snapshot

import "time"

type Snapshot struct {
	Host string `json:"host"`
	// Version of the agent that took it, to spot a mismatch with the gateway.
	AgentVersion string    `json:"agentVersion,omitempty"`
	TakenAt      time.Time `json:"takenAt"`
	// Modules the server has (helperapi.Mod*): sleep, backup, security…
	// The UI shows only what a server has.
	Features   []string    `json:"features"`
	BootAt     time.Time   `json:"bootAt"`
	Power      Power       `json:"power"`
	Containers []Container `json:"containers"`
	Backups    Backups     `json:"backups"`
	Disk       *Disk       `json:"disk,omitempty"`
	// CPU temperature samples in °C, oldest first, one per sample interval.
	Temps    []float64 `json:"temps"`
	Security *Security `json:"security,omitempty"`
	Timeline []Segment `json:"timeline"`
	// Apps installed by the app store, and its recent jobs (apps module).
	Apps []App `json:"apps,omitempty"`
	Jobs []Job `json:"jobs,omitempty"`
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

// App is one app the app store installed.
type App struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Tag       string    `json:"tag"`
	Runtime   string    `json:"runtime"`
	MainPort  int       `json:"mainPort"`
	HTTPS     bool      `json:"https,omitempty"`
	Installed time.Time `json:"installed"`
}

// Job is an app-store install or removal, running or recent.
type Job struct {
	ID       string     `json:"id"`
	Action   string     `json:"action"` // install, remove
	App      string     `json:"app"`
	Name     string     `json:"name"`
	State    string     `json:"state"` // running, done, failed
	Step     string     `json:"step"`
	Detail   string     `json:"detail,omitempty"`
	Error    string     `json:"error,omitempty"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
}

type Security struct {
	ClamAV *ClamAV `json:"clamav,omitempty"`
	// Addresses banned by the fail2ban jail; nil without one.
	Banned   *int            `json:"banned,omitempty"`
	Firewall []FirewallCheck `json:"firewall"`
}

// FirewallCheck is one configured rule check: OK when the chain holds the
// expected rule.
type FirewallCheck struct {
	Chain string `json:"chain"`
	OK    bool   `json:"ok"`
}

// FirewallFailing names the checks that failed.
func (s *Security) FirewallFailing() []string {
	var bad []string
	for _, c := range s.Firewall {
		if !c.OK {
			bad = append(bad, c.Chain)
		}
	}
	return bad
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
