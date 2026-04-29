package operations

import "errors"

// 書き込み系オペレーション（M05）の共通バリデーションエラー。
//
// なお ErrInvalidApp は M04 で records_query.go に既に定義されており、
// 後方互換のためここでは再定義しない（M06 着手前に集約検討）。
var (
	// ErrEmptyRecords は record_create で Record / Records どちらも未指定のとき。
	ErrEmptyRecords = errors.New("operations: at least one record is required")
	// ErrConflictingRecords は record_create で Record と Records が両方指定されたとき。
	ErrConflictingRecords = errors.New("operations: only one of Record / Records can be set")
	// ErrMissingUpdateKey は record_update で ID も UpdateKey も指定されない、または UpdateKey が片方のみのとき。
	ErrMissingUpdateKey = errors.New("operations: ID or (UpdateKeyField + UpdateKeyValue) is required")
	// ErrConflictingUpdateKey は record_update で ID と UpdateKey が両方指定されたとき。
	ErrConflictingUpdateKey = errors.New("operations: ID and UpdateKey cannot be specified together")
	// ErrEmptyRecord は record_update で Record が空のとき。
	ErrEmptyRecord = errors.New("operations: record fields are required")
	// ErrEmptyIDs は record_delete で IDs が空のとき。
	ErrEmptyIDs = errors.New("operations: at least one id is required")
	// ErrRevisionsLengthMismatch は record_delete で Revisions が指定された場合に len(IDs) と異なるとき。
	ErrRevisionsLengthMismatch = errors.New("operations: revisions length must match ids length")
	// ErrInvalidID は record_delete の IDs 要素が <= 0 のとき。
	ErrInvalidID = errors.New("operations: id must be > 0")
	// ErrConflictingAppRef は App と AppRef が両方指定されたとき（M08）。
	ErrConflictingAppRef = errors.New("operations: App and AppRef cannot be specified together")
	// ErrConflictingUpdateKeyFieldRef は UpdateKeyField と UpdateKeyFieldRef が両方指定されたとき（M08）。
	ErrConflictingUpdateKeyFieldRef = errors.New("operations: UpdateKeyField and UpdateKeyFieldRef cannot be specified together")
	// ErrResolverUnavailable は AppRef / UpdateKeyFieldRef が指定されたが Resolver が nil のとき（M08）。
	ErrResolverUnavailable = errors.New("operations: resolver is required when AppRef or UpdateKeyFieldRef is specified")
)
