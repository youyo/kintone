# 開発ワークフロー（devflow スキル群）

このリポジトリは **ロードマップ駆動開発** を採用。devflow スキル群でフローを自動化する。

## 全体像

```
spec → roadmap → plan → implement
```

## 利用シーン別

| 状況 | 使うスキル |
|------|-----------|
| プロダクト仕様書を作る | `/devflow:spec`（既に完了済み） |
| ロードマップを作る・追加する | `/devflow:roadmap`（既に M01〜M11 確定済み） |
| 単一マイルストーンの詳細計画を作る | `/devflow:plan` |
| 単一マイルストーンを実装する | `/devflow:implement` |
| 全マイルストーンを連続自律実行 | `/devflow:cycle` |
| 既存計画への意見・批評 | `/devflow:devils-advocate` → `/devflow:advocate` |

## マイルストーン状態管理

`plans/kintone-roadmap.md` の Progress セクションで管理:
- `[ ]` 未完了
- `[x]` 完了

各マイルストーンは前のマイルストーンの完了を待ってから着手（垂直スライス進行）。
完了時は roadmap のチェックボックスと Current Focus を必ず更新する。

## 詳細計画ファイル命名規則

- `plans/kintone-roadmap.md` — 全体ロードマップ
- `plans/kintone-m{NN}-{slug}.md` — マイルストーン別詳細計画
  - 例: `plans/kintone-m01-project-skeleton.md`
- M02 以降は着手時に `/devflow:plan` で生成（遅延生成）

## 自律ループ（devflow:cycle）の構造

各マイルストーンは独立したサブエージェントとして spawn され、以下のループを実行:

1. **Planner**: planning-agent が詳細計画を生成
2. **Evaluator (pre)**: devils-advocate → advocate → advisor() による品質ゲート
3. **Generator**: implementer が TDD で実装し `git commit`
4. **Evaluator (post)**: code-reviewer がレビュー
5. **CYCLE_RESULT.handoff** で次マイルストーンへ情報伝播

オーケストレーターは Edit/Write を直接行わず、Agent tool でのみ実装エージェントを起動する。

## 重要な参照ファイル

- 仕様書: `docs/specs/kintone_spec.md`
- ロードマップ: `plans/kintone-roadmap.md`
- M01 詳細計画: `plans/kintone-m01-project-skeleton.md`
- 全体ガイダンス: `CLAUDE.md`
