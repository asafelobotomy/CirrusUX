package registry

import (
	"regexp"
	"strconv"
	"strings"
)

// A tag is a candidate update for the pinned one only when it has the same
// shape: the same number of version numbers and the same suffix with its
// digits ignored ("35.0.2-apache" accepts "36.0.0-apache", never
// "36.0.0-fpm-alpine"; "12.4.20261026-211555" accepts "12.5.20261110-090000").
// Pre-releases are never offered, and neither is another build of the same
// version: the version numbers must rise. Digits in the suffix only choose
// between builds of a higher version, because some publishers restart their
// build counter (LinuxServer's "-ls329" came before "-ls4").

var (
	numsRe = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)(.*)$`)
	digits = regexp.MustCompile(`\d+`)
	preRe  = regexp.MustCompile(`(?i)(alpha|beta|rc|dev|nightly|unstable|preview|test|snapshot|canary|^[.-]?b\d)`)
)

type version struct {
	nums   []int
	suffix string // as written
	shape  string // suffix with digits replaced by 0, plus the number count
	extra  []int  // digits in the suffix, compared after nums
	pre    bool
}

func parse(tag string) (version, bool) {
	m := numsRe.FindStringSubmatch(tag)
	if m == nil {
		return version{}, false
	}
	var v version
	for _, p := range strings.Split(m[1], ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return version{}, false
		}
		v.nums = append(v.nums, n)
	}
	v.suffix = m[2]
	v.shape = strconv.Itoa(len(v.nums)) + ":" + digits.ReplaceAllString(v.suffix, "0")
	for _, d := range digits.FindAllString(v.suffix, -1) {
		n, _ := strconv.Atoi(d)
		v.extra = append(v.extra, n)
	}
	v.pre = preRe.MatchString(v.suffix)
	return v, true
}

// cmpInts compares two lists of numbers, a shorter one first when one starts
// the other.
func cmpInts(x, y []int) int {
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			return x[i] - y[i]
		}
	}
	return len(x) - len(y)
}

// less compares two versions of the same shape.
func less(a, b version) bool {
	if c := cmpInts(a.nums, b.nums); c != 0 {
		return c < 0
	}
	return cmpInts(a.extra, b.extra) < 0
}

// Newest returns the highest tag of the pinned tag's shape whose version
// numbers are higher than its, or "" when there is none.
func Newest(pinned string, tags []string) string {
	cur, ok := parse(pinned)
	if !ok {
		return ""
	}
	best, bestTag := cur, ""
	for _, t := range tags {
		v, ok := parse(t)
		if !ok || v.pre || v.shape != cur.shape || cmpInts(cur.nums, v.nums) >= 0 || !less(best, v) {
			continue
		}
		best, bestTag = v, t
	}
	return bestTag
}
