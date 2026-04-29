// Package output は JSON 出力規約の唯一の正典実装を提供する。
// 全 CLI コマンドはこのパッケージの API を通じて出力を行う。
//
// 成功時:  {"ok":true,"data":{...}}
// 失敗時:  {"ok":false,"error":{"code":"...","message":"..."}}
package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Error は失敗 JSON の "error" フィールドに格納される標準エラー形式。
// Code は SCREAMING_SNAKE_CASE（例: "USAGE", "INTERNAL", "CONFIG_NOT_FOUND"）。
// Details は任意の追加情報。省略時は JSON に出現しない（omitempty）。
type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// successEnvelope は成功レスポンスのエンベロープ。
// struct を使うことで JSON フィールド順序（ok → data）を保証する。
type successEnvelope struct {
	OK   bool `json:"ok"`
	Data any  `json:"data"`
}

// failureEnvelope は失敗レスポンスのエンベロープ。
// struct を使うことで JSON フィールド順序（ok → error）を保証する。
type failureEnvelope struct {
	OK    bool   `json:"ok"`
	Error *Error `json:"error"`
}

// Success は成功 JSON {"ok":true,"data":<data>} をエンコードして返す。
// data は nil 不可。空オブジェクトを表現したい場合は struct{}{} を渡す。
// 戻り値の []byte は末尾改行なし（Write が付与する）。
// HTML エスケープは無効化されており、LLM が読みやすい出力となる。
func Success(data any) ([]byte, error) {
	return encode(successEnvelope{OK: true, Data: data})
}

// Failure は失敗 JSON {"ok":false,"error":{...}} をエンコードして返す。
// e は nil 不可。nil を渡すと error が返る。
// 戻り値の []byte は末尾改行なし（Write が付与する）。
func Failure(e *Error) ([]byte, error) {
	if e == nil {
		return nil, errors.New("output: Failure called with nil *Error")
	}
	return encode(failureEnvelope{OK: false, Error: e})
}

// Write は payload を w に書き、末尾改行 1 つを保証する。
// 部分書き込みが発生した場合はエラーを返す。
func Write(w io.Writer, payload []byte) error {
	data := append(payload, '\n')
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return errors.New("output: short write")
	}
	return nil
}

// encode は v を JSON エンコードして返す。
// HTML エスケープは無効化する（SetEscapeHTML(false)）。
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// json.Encoder.Encode は末尾に \n を付与するので除去する
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b, nil
}
