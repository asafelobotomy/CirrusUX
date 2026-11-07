package gateway

import "testing"

func TestGitConfig(t *testing.T) {
	setEnv(t, map[string]string{"CIRRUS_DATA_DIR": "/d", "CIRRUS_GIT": "", "CIRRUS_GIT_DIR": "", "CIRRUS_GIT_URL": "", "CIRRUS_GIT_MAX_PUSH_MB": ""})
	c, err := ConfigFromEnv()
	if err != nil || c.Git {
		t.Fatalf("default: git=%v err=%v", c.Git, err)
	}
	setEnv(t, map[string]string{"CIRRUS_DATA_DIR": "/d", "CIRRUS_GIT": "1", "CIRRUS_GIT_URL": "https://homeserver.example.ts.net/"})
	c, err = ConfigFromEnv()
	if err != nil || !c.Git || c.GitDir != "/d/git" || c.GitMaxPush != 1024<<20 || c.GitURL != "https://homeserver.example.ts.net" {
		t.Errorf("on: %+v %v", c, err)
	}
	setEnv(t, map[string]string{"CIRRUS_GIT": "1", "CIRRUS_GIT_DIR": "/srv/git", "CIRRUS_GIT_MAX_PUSH_MB": "50"})
	if c, err = ConfigFromEnv(); err != nil || c.GitDir != "/srv/git" || c.GitMaxPush != 50<<20 {
		t.Errorf("set: %s %d %v", c.GitDir, c.GitMaxPush, err)
	}
	for k, v := range map[string]string{"CIRRUS_GIT_URL": "ftp://x", "CIRRUS_GIT_MAX_PUSH_MB": "0"} {
		setEnv(t, map[string]string{"CIRRUS_GIT": "1", k: v})
		if _, err := ConfigFromEnv(); err == nil {
			t.Errorf("%s=%s accepted", k, v)
		}
	}
	setEnv(t, map[string]string{"CIRRUS_GIT": "1", "CIRRUS_GIT_URL": "https://homeserver.example.ts.net/sub"})
	if _, err := ConfigFromEnv(); err == nil {
		t.Error("a URL with a path accepted")
	}
}
