# アーキテクチャ

`cmd/server` が依存関係を明示的に組み立て、Discord接続とEEW受信の起動・終了を管理する。`interfaces` はDiscordイベントとP2P地震情報を受信し、Discord固有の表示を担当する。`usecase` はダイス、キャラクター、システムユーザーの処理を機能別に提供し、`domain` はモデルと永続化契約を保持する。`infrastructure` は名前生成ライブラリとDBアクセスを実装し、`db/sqlc` は生成済みSQLを提供する。

依存は原則としてアダプターからユースケース、ドメインへ向ける。外部ライブラリの具体型をドメインに導入しない。新しい機能は必要なユースケースを追加し、Discordコマンドの入力・応答を `interfaces/discord_command.go` に登録する。永続化や外部サービスは利用側が必要とする契約を定義し、実装を `infrastructure` に置いて `cmd/server` で接続する。

依存の組み立ては手動で行う。旧Wire定義は空で生成コードも使用されておらず、この規模ではコード生成が不要である。EEWは既存の受信・通知実装を維持しており、詳細は [EEW.md](EEW.md) を参照。

検証: `go test ./...`、`go vet ./...`、`go test -race ./...`、`go build ./...`。外部サービスやMariaDBの実接続には別途環境が必要。
