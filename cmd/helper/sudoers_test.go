package main

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"cirrusux/internal/helperapi"
)

const helperBin = "/usr/local/sbin/cirrusux-helper"

// sudoersRules reads the command rules of deploy/server/sudoers-cirrusux:
// every item of every Cmnd_Alias, with "\" continuations joined.
func sudoersRules(t *testing.T) (rules []string, text string) {
	return rulesOf(t, "../../deploy/server/sudoers-cirrusux", "Cmnd_Alias")
}

// rulesOf reads the command lines a sudoers file allows: the lists on the
// lines that start with prefix.
func rulesOf(t *testing.T, file, prefix string) (rules []string, text string) {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text = string(b)
	joined := strings.ReplaceAll(text, "\\\n", " ")
	for _, line := range strings.Split(joined, "\n") {
		sep := "="
		if prefix != "Cmnd_Alias" {
			sep = "NOPASSWD:"
		}
		_, list, ok := strings.Cut(line, sep)
		if !ok || !strings.HasPrefix(strings.TrimSpace(line), prefix) {
			continue
		}
		for _, r := range strings.Split(list, ",") {
			if r = strings.Join(strings.Fields(r), " "); r != "" {
				rules = append(rules, r)
			}
		}
	}
	return rules, text
}

// matches is sudoers' wildcard match: "*" stands for any text.
func matches(rule, line string) bool {
	re := "^" + strings.ReplaceAll(regexp.QuoteMeta(rule), `\*`, ".*") + "$"
	return regexp.MustCompile(re).MatchString(line)
}

// sampleArgs is a valid argument list for each subcommand that takes any.
var sampleArgs = map[string][]string{
	helperapi.CmdAppJob:        {"0123456789abcdef"},
	helperapi.CmdArchiveDelete: {"navidrome-20261101-120000"},
	helperapi.CmdJournal:       {"cirrusux-agent.service"},
	helperapi.CmdSelfUpgrade:   {"v0.9.0"},
	helperapi.CmdSSHKeyAdd:     {"alice", "c3NoLWVkMjU1MTkgQUFBQUMzTnphQzFsWkRJMU5URTVBQUFBSUE"},
	helperapi.CmdSSHKeyRemove:  {"alice", "SHA256:" + strings.Repeat("A", 43)},
	helperapi.CmdAccountLock:   {"alice"},
	helperapi.CmdAccountUnlock: {"alice"},
	helperapi.CmdBuildJob:      {"0123456789abcdef"},
	helperapi.CmdBuildCancel:   {"0123456789abcdef"},
}

// internalCommands are started by systemd, never through sudo.
var internalCommands = []string{helperapi.CmdAppRun, helperapi.CmdOSRun, helperapi.CmdStorageRun, helperapi.CmdSelfRun, helperapi.CmdBuildRun, helperapi.CmdKeysIO}

// agentLines is every command line the agent builds for the helper.
func agentLines() [][]string {
	var out [][]string
	for _, sub := range helperapi.Commands {
		switch sub {
		case helperapi.CmdStayAwake:
			for _, a := range helperapi.StayAwakeArgs {
				out = append(out, []string{sub, a})
			}
		default:
			out = append(out, append([]string{sub}, sampleArgs[sub]...))
		}
	}
	return out
}

// Every command line the agent can run has a sudoers rule.
func TestSudoersCoversAgentCommands(t *testing.T) {
	rules, _ := sudoersRules(t)
	for _, args := range agentLines() {
		if !validArgs(args) {
			t.Errorf("the helper refuses %v", args)
		}
		line := helperBin + " " + strings.Join(args, " ")
		if !slices.ContainsFunc(rules, func(r string) bool { return matches(r, line) }) {
			t.Errorf("no sudoers rule allows %q", line)
		}
	}
}

// Every sudoers rule names a real subcommand the helper accepts, and
// nothing allows the subcommands only systemd starts.
func TestSudoersRulesAreReal(t *testing.T) {
	rules, text := sudoersRules(t)
	if len(rules) == 0 {
		t.Fatal("no rules found")
	}
	for _, r := range rules {
		f := strings.Fields(r)
		if f[0] != helperBin || len(f) < 2 {
			t.Errorf("rule %q is not a helper command line", r)
			continue
		}
		sub, args := f[1], f[2:]
		if !slices.Contains(helperapi.Commands, sub) {
			t.Errorf("rule %q: %s is not a helperapi command", r, sub)
			continue
		}
		if slices.Contains(internalCommands, sub) {
			t.Errorf("rule %q allows an internal subcommand", r)
		}
		// A lone "*" stands for the whole argument list.
		if len(args) == 1 && args[0] == "*" {
			args = sampleArgs[sub]
		}
		if !validArgs(append([]string{sub}, args...)) {
			t.Errorf("rule %q: the helper would refuse this command line", r)
		}
	}
	// The internal subcommands have no rule at all.
	for _, sub := range internalCommands {
		if strings.Contains(text, helperBin+" "+sub) {
			t.Errorf("sudoers names %s", sub)
		}
	}
	// The helper sets the same PATH sudo gives it.
	if !strings.Contains(text, `secure_path="`+rootPath+`"`) {
		t.Error("sudoers secure_path differs from the helper's rootPath")
	}
}

// The agent calls the helper by the path the sudoers rules name.
func TestAgentDefaultsToSudoersPath(t *testing.T) {
	b, err := os.ReadFile("../agent/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "sudo -n "+helperBin) {
		t.Errorf("cmd/agent doesn't default to sudo -n %s", helperBin)
	}
}

// stay-awake rules are written out in full: one per hold the panel offers.
func TestSudoersStayAwakeRulesAreExact(t *testing.T) {
	rules, _ := sudoersRules(t)
	for _, a := range helperapi.StayAwakeArgs {
		want := helperBin + " stay-awake " + a
		if !slices.Contains(rules, want) {
			t.Errorf("no exact rule %q", want)
		}
	}
	for _, r := range rules {
		if strings.HasPrefix(r, helperBin+" stay-awake") && strings.Contains(r, "*") {
			t.Errorf("stay-awake rule %q has a wildcard", r)
		}
	}
}

// The gateway machine's rule (split setup) allows the upgrade and the
// rollback, each as the helper accepts it, and nothing else.
func TestGatewaySudoersAllowsOnlyUpgrades(t *testing.T) {
	rules, text := rulesOf(t, "../../deploy/gateway/sudoers-cirrusux-upgrade", "cirrusux ALL=(root)")
	var subs []string
	for _, r := range rules {
		f := strings.Fields(r)
		if len(f) < 2 || f[0] != helperBin {
			t.Fatalf("rule %q is not a helper command line", r)
		}
		subs = append(subs, f[1])
		args := f[2:]
		if len(args) == 1 && args[0] == "*" {
			args = sampleArgs[f[1]]
		}
		if !validArgs(append([]string{f[1]}, args...)) {
			t.Errorf("rule %q: the helper would refuse this command line", r)
		}
	}
	slices.Sort(subs)
	want := slices.Clone(helperapi.GatewayCommands)
	slices.Sort(want)
	if !slices.Equal(subs, want) {
		t.Errorf("the gateway's rule allows %v, want %v", subs, want)
	}
	if !strings.Contains(text, `secure_path="`+rootPath+`"`) {
		t.Error("the gateway's secure_path differs from the helper's rootPath")
	}
}
