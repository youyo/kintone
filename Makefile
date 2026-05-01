.PHONY: test-quick test-integration test-e2e

# 通常テスト（build tag なし）
test-quick:
	go test -race -cover ./...

# integration テスト（外部 DB / network が必要なシナリオ）
test-integration:
	go test -race -tags=integration ./internal/store/...

# E2E テスト（in-process OIDC stub + kintone fake + storage backend）
test-e2e:
	go test -race -tags=e2e ./internal/cli/mcp/... ./internal/store/...
