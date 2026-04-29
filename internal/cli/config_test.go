package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/youyo/kintone/internal/cli"
)

// CC-1: config show が現在の解決済み設定を JSON で出力する
func TestConfigShow_BasicJSON(t *testing.T) {
	t.Setenv("KINTONE_DOMAIN", "foo.cybozu.com")
	t.Setenv("KINTONE_AUTH", "api-token")
	t.Setenv("KINTONE_API_TOKEN", "")
	// HOME を空ディレクトリへ
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"config", "show"}, &out, &errOut)
	if err != nil {
		t.Fatalf("unexpected error: %v, stdout=%q", err, out.String())
	}
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Profile string `json:"profile"`
			Domain  string `json:"domain"`
			Auth    string `json:"auth"`
			Source  struct {
				Domain string `json:"domain"`
			} `json:"source"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse stdout as JSON: %v, out=%q", err, out.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
	if resp.Data.Profile != "default" {
		t.Errorf("Profile = %q, want default", resp.Data.Profile)
	}
	if resp.Data.Domain != "foo.cybozu.com" {
		t.Errorf("Domain = %q, want foo.cybozu.com", resp.Data.Domain)
	}
	if resp.Data.Source.Domain != "env" {
		t.Errorf("Source.Domain = %q, want env", resp.Data.Source.Domain)
	}
}

// CC-2: api_token はマスクされる
func TestConfigShow_APITokenMasked(t *testing.T) {
	t.Setenv("KINTONE_API_TOKEN", "supersecret")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"config", "show"}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := out.String()
	if bytes.Contains(out.Bytes(), []byte("supersecret")) {
		t.Errorf("stdout should not contain raw token: %s", body)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"api_token":"***"`)) {
		t.Errorf("expected masked api_token in output, got: %s", body)
	}
}

// CC-3: --profile foo（file 未存在）で CONFIG_PROFILE_NOT_FOUND
func TestConfigShow_ProfileNotFound(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, ".config", "kintone", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(`
[profiles.default]
domain = "x.cybozu.com"
auth   = "api-token"
`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("KINTONE_API_TOKEN", "")

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"config", "show", "--profile", "prod"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error for missing profile, got nil")
	}
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("parse JSON: %v, out=%q", err, out.String())
	}
	if resp.Error.Code != "CONFIG_PROFILE_NOT_FOUND" {
		t.Errorf("Code = %q, want CONFIG_PROFILE_NOT_FOUND", resp.Error.Code)
	}
}

// CC-4: source 表示が ENV を反映する
func TestConfigShow_SourceTracking(t *testing.T) {
	t.Setenv("KINTONE_DOMAIN", "env.cybozu.com")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("KINTONE_API_TOKEN", "")

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"config", "show"}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"domain":"env"`)) {
		t.Errorf("expected source.domain=env, got: %s", out.String())
	}
}

// CC-5: config init が新規ファイルを作成する
func TestConfigInit_Create(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "init-test.toml")
	t.Setenv("KINTONE_API_TOKEN", "")

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"config", "init", "--config", cfgPath}, &out, &errOut)
	if err != nil {
		t.Fatalf("unexpected error: %v, out=%q", err, out.String())
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			Path    string `json:"path"`
			Created bool   `json:"created"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("parse JSON: %v, out=%q", err, out.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
	if !resp.Data.Created {
		t.Error("expected created=true")
	}
	if resp.Data.Path != cfgPath {
		t.Errorf("Path = %q, want %q", resp.Data.Path, cfgPath)
	}
}

// CC-6: 既存ファイルへの init は CONFIG_ALREADY_EXISTS
func TestConfigInit_AlreadyExists(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "exist.toml")
	if err := os.WriteFile(cfgPath, []byte("existing"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"config", "init", "--config", cfgPath}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error for existing file, got nil")
	}
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if resp.Error.Code != "CONFIG_ALREADY_EXISTS" {
		t.Errorf("Code = %q, want CONFIG_ALREADY_EXISTS", resp.Error.Code)
	}
	// 既存内容が破壊されていないこと
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "existing" {
		t.Errorf("file contents changed unexpectedly: %q", got)
	}
}

// CC-7: --force で既存ファイルを上書きできる
func TestConfigInit_ForceOverwrites(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "force.toml")
	if err := os.WriteFile(cfgPath, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"config", "init", "--config", cfgPath, "--force"}, &out, &errOut)
	if err != nil {
		t.Fatalf("unexpected error: %v, out=%q", err, out.String())
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) == "old" {
		t.Errorf("file should be overwritten, got: %q", got)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"overwritten":true`)) {
		t.Errorf("expected overwritten=true in output, got: %s", out.String())
	}
}

// CC-8: パーミッションが 0o600 で書き出される
func TestConfigInit_Permission0o600(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "perm.toml")

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"config", "init", "--config", cfgPath}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("Mode = %o, want 0600", mode)
	}
}

// CC-10: config init が ENV 経由のパスを使う
func TestConfigInit_UsesEnvPath(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "env-path.toml")
	t.Setenv("KINTONE_CONFIG_PATH", cfgPath)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"config", "init"}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v, out=%q", err, out.String())
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config file not created at env path: %v", err)
	}
}

// CC-11: config init が HOME ベースのデフォルトパスを使う
func TestConfigInit_DefaultPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("KINTONE_CONFIG_PATH", "")

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"config", "init"}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v, out=%q", err, out.String())
	}
	wantPath := filepath.Join(tmp, ".config", "kintone", "config.toml")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("config file not created at default path: %v", err)
	}
}

// CC-14: --config /nonexistent.toml で show すると CONFIG_NOT_FOUND
func TestConfigShow_ExplicitConfigNotFound(t *testing.T) {
	tmp := t.TempDir()
	missingPath := filepath.Join(tmp, "no-such.toml")
	t.Setenv("KINTONE_API_TOKEN", "")

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"config", "show", "--config", missingPath}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error for nonexistent config, got nil")
	}
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("parse JSON: %v, out=%q", err, out.String())
	}
	if resp.Error.Code != "CONFIG_NOT_FOUND" {
		t.Errorf("Code = %q, want CONFIG_NOT_FOUND", resp.Error.Code)
	}
	if got, _ := resp.Error.Details["path"].(string); got != missingPath {
		t.Errorf("Details.path = %v, want %q", resp.Error.Details["path"], missingPath)
	}
}

// CC-13: writeFileAtomic の rename 失敗をシミュレート
// （TempDir を read-only にして CreateTemp 失敗を起こす）
func TestConfigInit_TempCreateFailure(t *testing.T) {
	tmp := t.TempDir()
	// 親ディレクトリ自体は存在するが、子は存在しないパスを指定する
	// MkdirAll は成功するが、CreateTemp は親を read-only に変えれば失敗する
	cfgPath := filepath.Join(tmp, "sub", "f.toml")
	// 親ディレクトリ "sub" を予め作って read-only にする
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// cleanup 用に書込権限戻す
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(cfgPath), 0o700) })

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"config", "init", "--config", cfgPath}, &out, &errOut)
	if err == nil {
		t.Skip("read-only directory does not block CreateTemp on this platform")
	}
	// INTERNAL になるはず
	if !bytes.Contains(out.Bytes(), []byte(`"code":"INTERNAL"`)) {
		t.Errorf("expected INTERNAL code, got: %s", out.String())
	}
}

// CC-12: writeFileAtomic がディレクトリ作成失敗を伝播する
// （親ディレクトリが書き込み不可な状況をシミュレート）
func TestConfigInit_ParentMkdirFailure(t *testing.T) {
	tmp := t.TempDir()
	// 親をファイルにしてしまう
	parentAsFile := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(parentAsFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfgPath := filepath.Join(parentAsFile, "child.toml") // parent はファイルなので mkdir 失敗

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"config", "init", "--config", cfgPath}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error when parent is not a directory, got nil")
	}
	// INTERNAL になるはず（CONFIG_ で始まらない）
	if !bytes.Contains(out.Bytes(), []byte(`"code":"INTERNAL"`)) {
		t.Errorf("expected INTERNAL code, got: %s", out.String())
	}
}

// CL-1: config show で oauth_client_secret は "***" にマスクされること（M09）
func TestConfigShow_OAuthClientSecretMasked(t *testing.T) {
	t.Setenv("KINTONE_OAUTH_CLIENT_SECRET", "super-secret-oauth")
	t.Setenv("KINTONE_OAUTH_CLIENT_ID", "my-client-id")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("KINTONE_API_TOKEN", "")

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"config", "show"}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := out.String()
	if bytes.Contains(out.Bytes(), []byte("super-secret-oauth")) {
		t.Errorf("stdout should not contain raw client_secret: %s", body)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"oauth_client_secret":"***"`)) {
		t.Errorf("expected masked oauth_client_secret in output, got: %s", body)
	}
	// client_id はマスクしない
	if !bytes.Contains(out.Bytes(), []byte("my-client-id")) {
		t.Errorf("oauth_client_id should not be masked, got: %s", body)
	}
}

// CC-9: 壊れた TOML を --config で渡すと CONFIG_PARSE_ERROR
func TestConfigShow_ParseError(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "broken.toml")
	if err := os.WriteFile(cfgPath, []byte(`broken =`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Setenv("KINTONE_API_TOKEN", "")

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"config", "show", "--config", cfgPath}, &out, &errOut)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if resp.Error.Code != "CONFIG_PARSE_ERROR" {
		t.Errorf("Code = %q, want CONFIG_PARSE_ERROR", resp.Error.Code)
	}
	if got, _ := resp.Error.Details["path"].(string); got != cfgPath {
		t.Errorf("details.path = %v, want %q", resp.Error.Details["path"], cfgPath)
	}
}
