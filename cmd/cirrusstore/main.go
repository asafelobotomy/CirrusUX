// cirrusstore publishes a CirrusStore catalogue: a folder of signed files
// served over HTTPS as it is (GitHub Pages or any static host).
//
//	cirrusstore keygen -name NAME [-out DIR]
//	    a new key pair: NAME.key (keep it offline) and NAME.pub
//	cirrusstore sign -key NAME.key [-source NAME] [-days 180] [-dir .] DEF.json…
//	    wrap each app definition in a signed envelope in DIR/apps
//	cirrusstore index -key NAME.key [-source NAME] [-days 180] [-dir .]
//	    list DIR/apps in a new signed DIR/index.json
//	cirrusstore verify -pub NAME.pub [-source NAME] DIR
//	    check a folder the way a server will
//
// The source name defaults to the key's name. See docs/APPS.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cirrusux/internal/store"
	"cirrusux/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "index":
		err = index(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	case "version", "-version", "--version":
		fmt.Println("cirrusstore", version.String())
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cirrusstore:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  cirrusstore keygen -name NAME [-out DIR]
  cirrusstore sign -key NAME.key [-source NAME] [-days 180] [-dir .] DEF.json...
  cirrusstore index -key NAME.key [-source NAME] [-days 180] [-dir .]
  cirrusstore verify -pub NAME.pub [-source NAME] DIR`)
	os.Exit(2)
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	name := fs.String("name", "", "the key's name, usually the source's")
	out := fs.String("out", ".", "folder for NAME.key and NAME.pub")
	_ = fs.Parse(args)
	pub, sec, err := store.GenerateKey(*name, nil)
	if err != nil {
		return err
	}
	keyPath := filepath.Join(*out, *name+".key")
	f, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, sec.String()); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, *name+".pub"), []byte(pub.String()+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: keep it offline, and backed up\n%s.pub: %s\nfingerprint %s\n", keyPath, *name, pub, pub.Fingerprint())
	return nil
}

// signing are the flags sign and index share.
type signing struct {
	fs     *flag.FlagSet
	key    *string
	source *string
	days   *int
	dir    *string
}

func signingFlags(name string) signing {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	return signing{
		fs:     fs,
		key:    fs.String("key", "", "the signing key file (NAME.key)"),
		source: fs.String("source", "", "the source's name (default: the key's)"),
		days:   fs.Int("days", 180, "days until the signature expires"),
		dir:    fs.String("dir", ".", "the catalogue folder"),
	}
}

func (s signing) load() (store.Secret, string, time.Time, error) {
	if *s.key == "" {
		return store.Secret{}, "", time.Time{}, errors.New("-key is required")
	}
	b, err := os.ReadFile(*s.key)
	if err != nil {
		return store.Secret{}, "", time.Time{}, err
	}
	sec, err := store.ParseSecret(string(b))
	if err != nil {
		return sec, "", time.Time{}, err
	}
	source := *s.source
	if source == "" {
		source = sec.Name
	}
	if !store.ValidName(source) {
		return sec, "", time.Time{}, fmt.Errorf("source name %q", source)
	}
	if *s.days < 1 || *s.days > 3650 {
		return sec, "", time.Time{}, errors.New("-days: 1 to 3650")
	}
	return sec, source, time.Now().UTC().Add(time.Duration(*s.days) * 24 * time.Hour).Truncate(time.Second), nil
}

func sign(args []string) error {
	s := signingFlags("sign")
	_ = s.fs.Parse(args)
	sec, source, expires, err := s.load()
	if err != nil {
		return err
	}
	if s.fs.NArg() == 0 {
		return errors.New("sign: name the definitions to sign")
	}
	done, err := signApps(*s.dir, sec, source, expires, s.fs.Args())
	for _, d := range done {
		fmt.Println("signed", d)
	}
	if err == nil {
		fmt.Println("now run: cirrusstore index")
	}
	return err
}

func index(args []string) error {
	s := signingFlags("index")
	_ = s.fs.Parse(args)
	sec, source, expires, err := s.load()
	if err != nil {
		return err
	}
	ix, err := buildIndex(*s.dir, sec, source, expires, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("index.json: %s serial %d, %d apps, expires %s\n", ix.Source, ix.Serial, len(ix.Apps), ix.Expires.Format(time.DateOnly))
	return nil
}

func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	pubPath := fs.String("pub", "", "the public key file (NAME.pub)")
	source := fs.String("source", "", "the source's name (default: the key's)")
	_ = fs.Parse(args)
	if *pubPath == "" || fs.NArg() != 1 {
		return errors.New("verify: -pub NAME.pub DIR")
	}
	b, err := os.ReadFile(*pubPath)
	if err != nil {
		return err
	}
	key, err := store.ParseKey(strings.TrimSpace(string(b)))
	if err != nil {
		return err
	}
	name := *source
	if name == "" {
		name = key.Name
	}
	ix, err := verifyDir(fs.Arg(0), store.Source{Name: name, Keys: []store.Key{key}}, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("ok: %s serial %d, %d apps, expires %s\n", ix.Source, ix.Serial, len(ix.Apps), ix.Expires.Format(time.DateOnly))
	return nil
}
