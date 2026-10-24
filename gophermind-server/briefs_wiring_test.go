package main

import (
	"reflect"
	"testing"

	"gophermind/gophermind-lib/serve"
)

func TestBriefOptionsOffWithoutDir(t *testing.T) {
	opt := briefOptions(serverConfig{BriefCmd: "sudo -u gmrun gophermind"}, serve.Options{Token: "t"})
	if opt.BriefsDir != "" || opt.BriefCommand != nil {
		t.Errorf("routes must stay off without GOPHERMIND_BRIEFS_DIR: %+v", opt)
	}
}

func TestBriefOptionsLauncherPrefix(t *testing.T) {
	cfg := serverConfig{BriefsDir: "/var/lib/gophermind/briefs", BriefCmd: "sudo -n -u gmrun /usr/local/bin/gophermind"}
	opt := briefOptions(cfg, serve.Options{})
	if opt.BriefsDir != cfg.BriefsDir || opt.BriefCommand == nil || opt.BriefSignal != nil {
		t.Fatalf("options %+v", opt)
	}
	cmd := opt.BriefCommand([]string{"brief", "plan", "/x/brief.md", "--yes"})
	want := []string{"sudo", "-n", "-u", "gmrun", "/usr/local/bin/gophermind", "brief", "plan", "/x/brief.md", "--yes"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("args %v, want %v", cmd.Args, want)
	}
}

func TestBriefOptionsDefaultsToDirectLaunch(t *testing.T) {
	opt := briefOptions(serverConfig{BriefsDir: "/b"}, serve.Options{})
	if opt.BriefCommand != nil || opt.BriefSignal != nil {
		t.Error("with no prefix the serve package default (gophermind directly) applies")
	}
}

func TestBriefSettingsReadFromEnv(t *testing.T) {
	getenv := envMap(map[string]string{
		"GOPHERMIND_TOKEN": "t", "GOPHERMIND_BRIEFS_DIR": "/b", "GOPHERMIND_BRIEF_CMD": "x y", "GOPHERMIND_BRIEF_KILL_CMD": "k",
	})
	cfg, err := parseServerConfig(nil, getenv, fixedGetwd("/w"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BriefsDir != "/b" || cfg.BriefCmd != "x y" || cfg.BriefKillCmd != "k" {
		t.Errorf("cfg %+v", cfg)
	}
}
