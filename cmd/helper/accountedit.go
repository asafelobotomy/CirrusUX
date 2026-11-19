package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"cirrusux/internal/access"
	"cirrusux/internal/helperapi"
)

// accountCreate makes a login account with no password and no key: it
// can't be used until a key is added. The comment marks it as the panel's,
// so only such accounts are ever removed from the panel.
func accountCreate(ctx context.Context, name, role string) (helperapi.Result, error) {
	passwd, _ := os.ReadFile(rootFile("etc/passwd"))
	group, _ := os.ReadFile(rootFile("etc/group"))
	for _, text := range []string{string(passwd), string(group)} {
		for _, line := range strings.Split(text, "\n") {
			if n, _, _ := strings.Cut(line, ":"); n == name {
				return helperapi.Result{}, fmt.Errorf("the name %s is already used on %s", name, "this server")
			}
		}
	}
	args := []string{"--create-home", "--shell", "/bin/bash", "--comment", access.PanelComment}
	if role == "admin" {
		g := ""
		for _, c := range access.AdminGroups {
			if strings.Contains("\n"+string(group), "\n"+c+":") {
				g = c
				break
			}
		}
		if g == "" {
			return helperapi.Result{}, fmt.Errorf("no sudo, wheel or admin group exists here, so there is no admin role to give")
		}
		args = append(args, "--groups", g)
	}
	if res, err := run(ctx, "useradd", append(args, "--", name)...); err != nil {
		return res, err
	}
	return helperapi.Result{Output: []string{"created " + name + " (" + role + "): no password and no key yet, so nobody can log in as it until a key is added"}}, nil
}

// accountRemove removes an account the panel created, keeping its home
// folder and files: nothing is deleted.
func accountRemove(ctx context.Context, name string) (helperapi.Result, error) {
	u, err := person(name)
	if err != nil {
		return helperapi.Result{}, err
	}
	if !u.CreatedByPanel {
		return helperapi.Result{}, fmt.Errorf("%s wasn't created from the panel, so the panel won't remove it (userdel on the server does)", name)
	}
	if u.Admin {
		shadow, _ := os.ReadFile(rootFile("etc/shadow"))
		passwd, _ := os.ReadFile(rootFile("etc/passwd"))
		group, _ := os.ReadFile(rootFile("etc/group"))
		all := access.ParseAccounts(string(passwd), string(group))
		access.ApplyShadow(all, string(shadow), time.Now().Unix()/86400)
		if !otherAdminCanSignIn(ctx, all, name) {
			return helperapi.Result{}, fmt.Errorf("%s is the only admin who can log in; removing it would shut everyone out", name)
		}
	}
	if res, err := run(ctx, "userdel", "--", name); err != nil {
		return res, err
	}
	return helperapi.Result{Output: []string{"removed " + name + "; its home folder " + u.Home + " is kept"}}, nil
}
