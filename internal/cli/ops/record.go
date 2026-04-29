package ops

import (
	"net/http"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/service/operations"
)

// dryRunData は --dry-run 時に出力する JSON 構造。
//
// **重要（advisor 指摘 #5）**: Body は kintoneapi.BuildXxxBody から取得し、
// 実 API 送信時と byte 完全一致させる。
type dryRunData struct {
	DryRun bool           `json:"dry_run"`
	Method string         `json:"method"`
	Path   string         `json:"path"`
	Body   map[string]any `json:"body"`
}

// newRecordCmd は `kintone ops record` ツリーを構築する。
func newRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record",
		Short: "レコードの新規登録 / 更新 / 削除（書き込み系）",
	}
	cmd.AddCommand(newRecordCreateCmd())
	cmd.AddCommand(newRecordUpdateCmd())
	cmd.AddCommand(newRecordDeleteCmd())
	return cmd
}

// newRecordCreateCmd は `kintone ops record create` を構築する。
//
// フラグ:
//
//	--app           int64    必須
//	--record-json   string   単件 JSON（例: '{"name":{"value":"foo"}}'）
//	--records-json  string   複数件 JSON（例: '[{...},{...}]'）
//	--dry-run       bool     送信せずリクエスト body を JSON 出力
//
// バリデーション:
//   - --record-json と --records-json の両方未指定は USAGE
//   - 両方指定も USAGE
func newRecordCreateCmd() *cobra.Command {
	var (
		app         int64
		recordJSON  string
		recordsJSON string
		dryRun      bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "レコードを新規登録する（複数件可・dry-run 対応）",
		Long: `POST /k/v1/records.json を呼び、--record-json または --records-json で渡したレコードを登録します。

--dry-run を付けると API を呼ばず、送信予定の HTTP body を JSON で表示します。
（実 API 送信時と body は byte 完全一致します。）`,
		RunE: func(cmd *cobra.Command, args []string) error {
			records, err := normalizeRecords(recordJSON, recordsJSON)
			if err != nil {
				return err
			}
			req := kintoneapi.InsertRecordsRequest{App: app, Records: records}

			if dryRun {
				return writeDryRun(cmd, http.MethodPost, "/k/v1/records.json",
					kintoneapi.BuildInsertRecordsBody(req))
			}

			a, err := buildAPI(cmd)
			if err != nil {
				return err
			}
			out, err := operations.RecordCreate(cmd.Context(), a, operations.RecordCreateInput{
				App: app, Records: records,
			})
			if err != nil {
				return err
			}
			return writeJSON(cmd, out)
		},
	}
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（必須）")
	cmd.Flags().StringVar(&recordJSON, "record-json", "", "単件レコード JSON")
	cmd.Flags().StringVar(&recordsJSON, "records-json", "", "複数件レコード JSON 配列")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "送信せずリクエスト body を JSON 出力")
	_ = cmd.MarkFlagRequired("app")
	return cmd
}

// normalizeRecords は --record-json / --records-json から最終的な records 配列を生成する。
func normalizeRecords(single, multi string) ([]map[string]any, error) {
	hasSingle := single != ""
	hasMulti := multi != ""
	if !hasSingle && !hasMulti {
		return nil, newUsageError("either --record-json or --records-json is required")
	}
	if hasSingle && hasMulti {
		return nil, newUsageError("only one of --record-json / --records-json can be specified")
	}
	if hasSingle {
		m, err := parseRecordJSON(single)
		if err != nil {
			return nil, err
		}
		return []map[string]any{m}, nil
	}
	return parseRecordsJSON(multi)
}

// newRecordUpdateCmd は `kintone ops record update` を構築する。
//
// フラグ:
//
//	--app                int64   必須
//	--id                 int64   ID 指定パス（updateKey と排他）
//	--update-key-field   string  updateKey パス（field code）
//	--update-key-value   string  updateKey パス（value）
//	--record-json        string  必須
//	--revision           int64   任意（楽観ロック）
//	--dry-run            bool
func newRecordUpdateCmd() *cobra.Command {
	var (
		app            int64
		id             int64
		updateKeyField string
		updateKeyValue string
		recordJSON     string
		revision       int64
		dryRun         bool
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "レコード単件を更新する（id / updateKey 排他・dry-run 対応）",
		Long: `PUT /k/v1/record.json を呼び、--id または --update-key-field/--update-key-value で
特定したレコードを --record-json の内容で更新します。--revision を指定すると楽観ロックを行います。

--dry-run を付けると API を呼ばず、送信予定の HTTP body を JSON で表示します。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			hasID := id > 0
			hasKeyField := updateKeyField != ""
			hasKeyValue := updateKeyValue != ""
			hasKey := hasKeyField || hasKeyValue
			if hasID && hasKey {
				return newUsageError("--id and --update-key-* are mutually exclusive")
			}
			if !hasID {
				if !hasKeyField || !hasKeyValue {
					return newUsageError("either --id or both --update-key-field and --update-key-value are required")
				}
			}
			rec, err := parseRecordJSON(recordJSON)
			if err != nil {
				return err
			}

			req := kintoneapi.UpdateRecordRequest{
				App:    app,
				Record: rec,
			}
			if hasID {
				req.ID = id
			} else {
				req.UpdateKey = &kintoneapi.UpdateKey{
					Field: updateKeyField,
					Value: updateKeyValue,
				}
			}
			// --revision はフラグが明示的に変更されたときのみ送る（0 と未指定の区別）。
			if cmd.Flags().Changed("revision") {
				rv := revision
				req.Revision = &rv
			}

			if dryRun {
				return writeDryRun(cmd, http.MethodPut, "/k/v1/record.json",
					kintoneapi.BuildUpdateRecordBody(req))
			}

			a, err := buildAPI(cmd)
			if err != nil {
				return err
			}
			opIn := operations.RecordUpdateInput{
				App:            app,
				ID:             id,
				UpdateKeyField: updateKeyField,
				UpdateKeyValue: updateKeyValue,
				Record:         rec,
			}
			if cmd.Flags().Changed("revision") {
				rv := revision
				opIn.Revision = &rv
			}
			out, err := operations.RecordUpdate(cmd.Context(), a, opIn)
			if err != nil {
				return err
			}
			return writeJSON(cmd, out)
		},
	}
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（必須）")
	cmd.Flags().Int64Var(&id, "id", 0, "更新対象レコード ID（updateKey と排他）")
	cmd.Flags().StringVar(&updateKeyField, "update-key-field", "", "updateKey: フィールドコード")
	cmd.Flags().StringVar(&updateKeyValue, "update-key-value", "", "updateKey: 値")
	cmd.Flags().StringVar(&recordJSON, "record-json", "", "更新内容 JSON（必須）")
	cmd.Flags().Int64Var(&revision, "revision", 0, "楽観ロック用 revision（フラグ未指定なら送信しない）")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "送信せずリクエスト body を JSON 出力")
	_ = cmd.MarkFlagRequired("app")
	_ = cmd.MarkFlagRequired("record-json")
	return cmd
}

// newRecordDeleteCmd は `kintone ops record delete` を構築する。
//
// フラグ:
//
//	--app        int64    必須
//	--id         int64[]  必須・複数指定可（--id 1 --id 2）
//	--revision   int64[]  任意・複数指定可（指定時 --id と同要素数）
//	--dry-run    bool
func newRecordDeleteCmd() *cobra.Command {
	var (
		app       int64
		ids       []int64
		revisions []int64
		dryRun    bool
	)
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "レコードを複数件削除する（dry-run 対応）",
		Long: `DELETE /k/v1/records.json を呼び、--id で指定したレコードを削除します。
--revision を指定すると楽観ロックを行います（--id と同じ個数を指定）。

--dry-run を付けると API を呼ばず、送信予定の HTTP body を JSON で表示します。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// advisor 指摘 #6: cobra の Int64SliceVar + MarkFlagRequired の挙動は version 依存。
			// 確実に空 array を弾くため RunE 冒頭で明示判定する。
			if len(ids) == 0 {
				return newUsageError("--id is required (specify one or more times: --id 1 --id 2)")
			}
			if len(revisions) > 0 && len(revisions) != len(ids) {
				return newUsageError("--revision count (%d) must match --id count (%d)", len(revisions), len(ids))
			}
			req := kintoneapi.DeleteRecordsRequest{App: app, IDs: ids, Revisions: revisions}

			if dryRun {
				return writeDryRun(cmd, http.MethodDelete, "/k/v1/records.json",
					kintoneapi.BuildDeleteRecordsBody(req))
			}

			a, err := buildAPI(cmd)
			if err != nil {
				return err
			}
			out, err := operations.RecordDelete(cmd.Context(), a, operations.RecordDeleteInput{
				App: app, IDs: ids, Revisions: revisions,
			})
			if err != nil {
				return err
			}
			return writeJSON(cmd, out)
		},
	}
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（必須）")
	cmd.Flags().Int64SliceVar(&ids, "id", nil, "削除対象レコード ID（必須・複数指定可: --id 1 --id 2）")
	cmd.Flags().Int64SliceVar(&revisions, "revision", nil, "楽観ロック用 revision（任意・--id と同要素数）")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "送信せずリクエスト body を JSON 出力")
	_ = cmd.MarkFlagRequired("app")
	return cmd
}

// writeDryRun は dry-run の出力を JSON で書き出す。
func writeDryRun(cmd *cobra.Command, method, path string, body map[string]any) error {
	payload, err := output.Success(dryRunData{
		DryRun: true,
		Method: method,
		Path:   path,
		Body:   body,
	})
	if err != nil {
		return err
	}
	return output.Write(cmd.OutOrStdout(), payload)
}

// writeJSON は data を {"ok":true,"data":...} 形式で書き出す。
func writeJSON(cmd *cobra.Command, data any) error {
	payload, err := output.Success(data)
	if err != nil {
		return err
	}
	return output.Write(cmd.OutOrStdout(), payload)
}
