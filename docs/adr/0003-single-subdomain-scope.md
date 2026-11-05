# ADR 0003: 1 デプロイ = 1 つの cybozu.com サブドメイン、ゲストスペースは対象外

- 状態: 提案(M01 で採用予定)。環境変数名 `KINTONE_HOST`(ホスト全体)はユーザー決定により確定(2026-11-05)
- 日付: 2026-11-05
- 関連: logbutler(単一 Backlog スペース。ロードマップ「決定済み事項」)、Go `internal/config`(`KINTONE_DOMAIN` 単一)

## 状況

kintone の API は `https://<サブドメイン>.cybozu.com/k/v1/...`。ゲストスペース内のアプリは `/k/guest/<GUEST_SPACE_ID>/v1/...` と別のパスになる(公式 https://cybozu.dev/ja/kintone/docs/rest-api/records/get-records/ ほか各ページ)。
OAuth クライアントはサブドメインごとに cybozu.com 共通管理で登録する(公式)。Go 版も 1 プロセス 1 ドメインだった(`KINTONE_DOMAIN`)。

## 決定

1. **1 デプロイ = 1 つの kintone ホスト**(環境変数 `KINTONE_HOST`、例 `example.cybozu.com`)。複数サブドメインを使うなら Worker を複数デプロイする。
2. **ゲストスペースは対象外**。`/k/guest/<id>/` を組み立てる引数・設定は作らない。
3. ホスト名は `.cybozu.com` に限定せず、形式(スキーム・パスなし)だけを検査する(kintone.com / cybozu.cn / 開発環境でも使えるように)。ただし `.cybozu.com` 以外は検証していない。

## 理由

- 利用者・OAuth クライアント・IP 制限の単位がサブドメインで、Worker 1 つが複数テナントを扱う必要がない。テナントをまたぐ設定ミス(別テナントへ認可情報を送る)を構造的に避けられる。
- ゲストスペースは URL の形・利用者の種別(ゲストユーザーは REST API でセッション認証しか使えない=公式の認証ページ)が異なり、OAuth で扱えるか自体が未確認。範囲を広げる根拠がない。

## 既知の限界(ユーザーに見える挙動)

- `GET /k/v1/apps.json` は、ゲストスペースを指定しない通常ユーザーの呼び出しに、閲覧可能なゲストスペース内のアプリも含めて返す(公式 https://cybozu.dev/ja/kintone/docs/rest-api/apps/get-apps/ )。そのため `kintone_apps_search` に出たアプリを `kintone_records_query` に渡すと、ゲストスペースのアプリだけ失敗する(`not_found` など。V13)。ツールの description に「ゲストスペースのアプリは扱えない」と書く。
- cybozu.com 共通管理の **IP アドレス制限**を使う環境では、OAuth クライアント(= Worker)の IP を許可する必要がある(公式 add-client ページ)。Workers の送信元 IP は固定でなく、許可リストに載せにくい。認可時の疎通確認(`apps.json?limit=1`)で早期に気づける。IP 制限下での可否は未検証(V10)。
- Go 版のゲストスペース対応は元々なく、退行ではない(`grep -rn guest internal` に該当なし)。

## 却下した案

- 複数サブドメインを 1 Worker で扱う(grant ごとにホストを持つ): 認可画面でホスト選択が必要になり、DCR クライアントの信頼境界が曖昧になる。
- ゲストスペース対応(`guest_space_id` 引数): 未検証の前提が多すぎる。需要が出てから別 ADR。

## 未検証の前提

- `.cybozu.com` 以外のドメイン(kintone.com、cybozu.cn)での OAuth エンドポイントと動作(V10 の補足)
- ゲストスペースのアプリに対する失敗の実際のステータスとコード(V13)
