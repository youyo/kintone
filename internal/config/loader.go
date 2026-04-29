package config

import (
	"errors"
	"io/fs"

	"github.com/BurntSushi/toml"
)

// LoadFile は path から TOML を読み取り FileConfig を返す。
//
// path が存在しない場合（fs.ErrNotExist）は (zero, nil) を返す（エラーにしない）。
// 「明示指定（--config / KINTONE_CONFIG_PATH）かつ不在」を NotFoundError に
// 変換する責務は呼び出し側（Load）にあり、このレイヤでは IO の事実のみ扱う。
//
// パースエラー時は *ParseError でラップする。
// その他の IO エラー（permission denied 等）はそのまま伝播。
//
// readFile は os.ReadFile 互換のシグネチャで、テスト時にモック化可能。
// nil の場合は呼び出し側責任（Load 経由なら必ず非 nil）。
func LoadFile(path string, readFile func(string) ([]byte, error)) (FileConfig, error) {
	bytes, err := readFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return FileConfig{}, nil
		}
		return FileConfig{}, err
	}
	if len(bytes) == 0 {
		return FileConfig{}, nil
	}
	return decodeTOML(path, bytes)
}

// decodeTOML は TOML バイト列を FileConfig へデコードする。
// パースエラー時は *ParseError でラップする。
func decodeTOML(path string, bytes []byte) (FileConfig, error) {
	var fc FileConfig
	if _, decErr := toml.Decode(string(bytes), &fc); decErr != nil {
		return FileConfig{}, &ParseError{Path: path, Err: decErr}
	}
	return fc, nil
}

// ParseError は TOML パース失敗を表す。
// errors.As / errors.Unwrap で原因 error を取得できる。
type ParseError struct {
	Path string
	Err  error
}

// Error は人間可読なエラーメッセージを返す。
func (e *ParseError) Error() string {
	return "config: parse error in " + e.Path + ": " + e.Err.Error()
}

// Unwrap は errors.Unwrap / errors.Is / errors.As 用の原因 error を返す。
func (e *ParseError) Unwrap() error { return e.Err }

// NotFoundError は明示指定された config ファイルが存在しないことを表す。
// CLI 層で CONFIG_NOT_FOUND コードに変換される。
type NotFoundError struct {
	Path string
}

// Error は人間可読なエラーメッセージを返す。
func (e *NotFoundError) Error() string {
	return "config: file not found: " + e.Path
}

// AlreadyExistsError は config init 時に既存ファイルがあった場合のエラー。
// CLI 層で CONFIG_ALREADY_EXISTS コードに変換される。
type AlreadyExistsError struct {
	Path string
}

// Error は人間可読なエラーメッセージを返す。
func (e *AlreadyExistsError) Error() string {
	return "config: file already exists: " + e.Path
}

// errIs は errors.Is の薄ラッパ（other パッケージから呼ぶための内部用）。
func errIs(err, target error) bool {
	return errors.Is(err, target)
}
