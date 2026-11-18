package main

import (
	"fmt"
	"net/netip"
	"strconv"

	"cirrusux/internal/build"
)

// buildConf is the builds module's settings in helper.env. Builds use the
// app store's container runtime (CONTAINER_RUNTIME).
type buildConf struct {
	On         bool
	Subnet     string
	MemoryMB   int
	CPUs       int
	MaxMinutes int
}

// Defaults for a step's container and a build's length.
const (
	defaultBuildMemoryMB = 2048
	defaultBuildCPUs     = 2
	defaultBuildMinutes  = 60
	buildPids            = 1024
)

func (b buildConf) limits() build.Limits {
	l := build.Limits{MemoryMB: b.MemoryMB, CPUs: b.CPUs, Pids: buildPids}
	if l.MemoryMB == 0 {
		l.MemoryMB = defaultBuildMemoryMB
	}
	if l.CPUs == 0 {
		l.CPUs = defaultBuildCPUs
	}
	return l
}

func (b buildConf) subnet() string {
	if b.Subnet == "" {
		return build.DefaultSubnet
	}
	return b.Subnet
}

func (b buildConf) maxMinutes() int {
	if b.MaxMinutes == 0 {
		return defaultBuildMinutes
	}
	return b.MaxMinutes
}

func init() {
	keys["BUILDS"] = func(c *conf, v string) error {
		if v != "yes" && v != "no" {
			return fmt.Errorf("yes or no")
		}
		c.Build.On = v == "yes"
		return nil
	}
	keys["BUILD_SUBNET"] = func(c *conf, v string) error {
		p, err := netip.ParsePrefix(v)
		if err != nil || !p.Addr().Is4() || p.Bits() < 16 || p.Bits() > 28 || p.Masked() != p {
			return fmt.Errorf("an IPv4 network such as %s", build.DefaultSubnet)
		}
		c.Build.Subnet = v
		return nil
	}
	keys["BUILD_MEMORY_MB"] = number(256, 1<<20, func(c *conf) *int { return &c.Build.MemoryMB })
	keys["BUILD_CPUS"] = number(1, 256, func(c *conf) *int { return &c.Build.CPUs })
	keys["BUILD_MAX_MINUTES"] = number(1, build.MaxMinutes, func(c *conf) *int { return &c.Build.MaxMinutes })
}

func number(lo, hi int, field func(*conf) *int) func(*conf, string) error {
	return func(c *conf, v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < lo || n > hi {
			return fmt.Errorf("a whole number from %d to %d", lo, hi)
		}
		*field(c) = n
		return nil
	}
}
