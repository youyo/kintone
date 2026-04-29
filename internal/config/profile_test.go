package config

import "testing"

func TestSelectProfile_CLIPriority(t *testing.T) {
	t.Parallel()
	cli := CLIConfig{Profile: "cli-p"}
	env := EnvConfig{Profile: "env-p"}
	file := &FileConfig{DefaultProfile: DefaultProfileBlock{Name: "file-p"}}
	if got := SelectProfile(cli, env, file); got != "cli-p" {
		t.Errorf("SelectProfile = %q, want %q", got, "cli-p")
	}
}

func TestSelectProfile_ENVOverFile(t *testing.T) {
	t.Parallel()
	cli := CLIConfig{}
	env := EnvConfig{Profile: "env-p"}
	file := &FileConfig{DefaultProfile: DefaultProfileBlock{Name: "file-p"}}
	if got := SelectProfile(cli, env, file); got != "env-p" {
		t.Errorf("SelectProfile = %q, want %q", got, "env-p")
	}
}

func TestSelectProfile_FileDefault(t *testing.T) {
	t.Parallel()
	cli := CLIConfig{}
	env := EnvConfig{}
	file := &FileConfig{DefaultProfile: DefaultProfileBlock{Name: "file-p"}}
	if got := SelectProfile(cli, env, file); got != "file-p" {
		t.Errorf("SelectProfile = %q, want %q", got, "file-p")
	}
}

func TestSelectProfile_AllEmptyFallsBackToDefault(t *testing.T) {
	t.Parallel()
	cli := CLIConfig{}
	env := EnvConfig{}
	file := &FileConfig{}
	if got := SelectProfile(cli, env, file); got != "default" {
		t.Errorf("SelectProfile = %q, want %q", got, "default")
	}
}

func TestSelectProfile_NilFile(t *testing.T) {
	t.Parallel()
	if got := SelectProfile(CLIConfig{}, EnvConfig{}, nil); got != "default" {
		t.Errorf("SelectProfile(nil) = %q, want %q", got, "default")
	}
}
