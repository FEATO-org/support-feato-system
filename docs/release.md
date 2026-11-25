# SFS コンテナリリース運用

SFS のコンテナは GitHub Actions の **Release SFS** から公開する。本番デプロイは FEATO インフラ側で別途実施する。

## 開発と CI

作業ブランチから `develop` へ PR を出す。反映済みの変更は `develop` から `main` へ PR で取り込む。両ブランチ向けの PR と両ブランチへの push で CI が実行される。CI は Go モジュールの検証、Go テスト、vet、アプリケーションのビルド、リリースロジックの単体テスト、本番用イメージの Buildx ビルドを行う。既存の CodeQL も利用する。

Go テストは EEW のライフサイクル、受信コードの選別、再接続とキャンセルを扱う。Discord・DB・Google API の実サービスを使う結合テストは現時点のテスト構成に含まれない。CI とリリースの Go バージョン、および Buildx の `GO_VERSION` は対象コミットの `.go-version`（導入時点では既存ビルドと同じ `1.27.4`）から得る。 `go.mod` の `go` 行は最低要件（導入時点では `1.26.0`）を表す。導入前のコミットを `resume` で再ビルドする場合はそのコミットの Compose の `GO_VERSION` を使う。今後 Go を更新するときはローカル Compose の指定も合わせて確認する。

## 手動リリース

GitHub の **Actions → Release SFS → Run workflow** で実行ブランチに `main` を選ぶ。

| モード | 入力と用途 |
| --- | --- |
| `dry-run` | `version` を空欄にする。対象 SHA、採番、競合、Go テスト、OCI イメージのビルドとメタデータ、GitHub の Release Notes 生成を確認する。予定バージョンは予約しない。 |
| `release` | `version` を空欄にする。手動実行時点の `main` の SHA を固定し、改めて採番する。時間が経過した場合は実行前に対象 SHA を確認する。 |
| `resume` | 既に途中まで公開された `version` を入力する。Git タグまたは正式イメージから元の SHA を復元し、実行時の `main` の履歴に含まれることを確認する。 |

CalVer は東京の日付を使う `YYYY-MM-DD.N`。Git タグ、GitHub Release、GHCR タグから同日の最大連番を調べ、1 を加える。初回は 1。採番後に日付が変わっても同じバージョンで処理する。正式公開前に状態を再確認する。通常リリースでは CalVer タグまたは正式イメージに対応する同一 SHA の再公開を拒否する。計画ジョブは、読み取り権限では取得できない下書きも含めて採番する。公開直前の再確認で競合が見つかれば停止する。

同じ Workflow の各モードは 1 つの concurrency group を共有する。実行中の run は継続する。待機中の run は後から投入された run に置き換わる場合があり、実行順は保証されない。Job Summary で実行状況を確認し、キャンセルされた依頼は必要に応じて再 dispatch する。

## イメージと公開順序

イメージは `ghcr.io/feato-org/support-feato-system` の `linux/amd64`。Buildx は既存 Dockerfile の本番ステージを使い、Git に追跡された対象 SHA のファイルをビルドコンテキストにする。現在の Dockerfile は Discord 用の空の環境変数を宣言している。認証情報は実行時にインフラ側で注入する。

| タグ | 意味 |
| --- | --- |
| `YYYY-MM-DD.N` | 正式イメージ。Git タグと同名。 |
| `sha-<40桁のSHA>` | ソースコミットに対応する正式イメージ。 |
| `latest` | 最新の正式リリースへ移動するタグ。 |
| `main` | 既存のデプロイとの互換用。新方式では最新の正常な正式リリースを参照する。main ブランチへの push で更新される旧方式から切り替わる。 |

テスト成功後、ビルド済みの OCI アーカイブを検査し、同じバイト列を公開ジョブへ渡す。公開ジョブは Git タグを作成し、GHCR へ Digest 指定で manifest を転送する。その manifest を正式タグ、SHA タグに順に設定して Digest を照合し、GitHub Release を公開する。最後に `latest`、`main` を順に更新し、各タグの Digest を確認する。OCI ラベルに source、version、revision を記録する。SBOM と Provenance は、単一 manifest の Digest を維持する配布経路を先に導入するため見送った。attestation を載せた index の転送と Swarm での解決は別途検証する。

GitHub の Git ref 作成は同名 ref があると失敗する。GHCR のタグには、この運用で使える原子的な比較更新がないため、状態を読み直してから書き込み、書き込み後に照合する。リリース中に人や別 Workflow が同じバージョン・SHA・エイリアスを書き換えないようにする。複数タグの更新は個別に行われるため、通信障害時には一時的に Digest がそろわない場合がある。

## GitHub Release Notes

GitHub の自動生成機能に、直前の公開済み正式 Release の Git タグを渡す。そのリリースは Git タグ、正式イメージ、SHA タグ、Release 本文が整合していることを確認する。Git タグだけが残った失敗や Draft／Prerelease は比較対象から除く。正式イメージを取得できる Release の公開を基準とするため、可変タグの反映遅れは復旧対象として個別に扱う。初回は既存の Git タグ `0.1.0` を比較元に使う。Git タグ・Release の履歴が空なら GitHub の初回用の自動生成に任せる。既存履歴があり、適切な基準タグを確認できない場合は停止する。

カテゴリは `.github/release.yml` で管理する。現行の `bug`、`dependencies`、`enhancement`、`documentation` を分類に使う。`feature`、`security` は同名ラベルを追加・付与すると専用カテゴリに入る。ラベルのない PR も「その他」に含める。

## 初回導入

1. 導入 PR をレビューし、`develop` に取り込む。既定ブランチは `develop`。次に `develop` から `main` 向けの PR を準備し、CI と CodeQL を確認する。調査時点で既存の CodeQL Workflow は GitHub 上で `disabled_inactivity` だったため、管理者が Actions から再有効化し、解析結果を確認する。
2. **main への導入 PR をマージする前に** 管理者が旧 **Create and publish a Docker image**（`upload.yml`、Workflow ID `56448257`）を無効化する。旧 run がすべて完了したことを確認し、過去 run の再実行も止める。既存の `:main` の Digest を記録しておく。新しい公開ジョブは旧 Workflow の停止と実行状況を確認できなければ停止する。
3. 導入 PR を `main` に取り込む。両ブランチの Workflow、`main` の CI、および `main` への取り込みだけでは新しいイメージが公開されないことを確認する。
4. `main` の **Release SFS** で `dry-run` を実行する。Job Summary で固定 SHA、CalVer、Go テスト、ビルド、OCI Digest、Notes プレビューの成功を確認する。Git タグ、GHCR タグ、GitHub Release に変化がないことを照合する。既存ブランチに Workflow が未導入の場合、この確認は導入後に実施する。
5. 承認を得た正式公開枠で `release` を実行する。Job Summary、Git タグ、GitHub Release 本文、4 タグの Digest を照合する。正式参照を検証環境で pull し、運用チームとデプロイを調整する。正式公開の権限と GHCR 書き込みはこの段階で初めて実環境で実証される。

権限は `GITHUB_TOKEN` を使用する。CI とビルドは `contents: read`。計画ジョブは下書きの参照と Dry-run の generate-notes API に必要な `contents: write` だけを持つ。この API の返すプレビューは保存されない。公開ジョブは `contents: write`、`packages: write`、旧 Workflow 状態の取得に `actions: read` を使う。Actions のトークンで作った Git タグから別 Workflow が起動することには依存しない。既存 GHCR パッケージの **Manage Actions access** で SFS に Write があること、またはリポジトリからの権限継承を確認する。確認できない場合はパッケージ管理者に確認を依頼する。将来 CalVer 用タグ Ruleset を追加する場合は、この Workflow にタグ作成を許可する方法を管理者が決める。失敗時は権限や Ruleset を確認し、制限の迂回や既存タグの付け替えを避ける。

## 障害復旧

1. 失敗した run の Job Summary でバージョンと SHA を確認し、Git タグ、GHCR の正式・SHA タグと Digest、GitHub Release、可変タグの状態を照合する。そろっていない場合も、API 障害と不存在を区別する。
2. テスト・ビルド失敗などで Git タグ、正式イメージ、Release のいずれも存在しなければ、原因を解消して `release` を新規 dispatch する。公開処理が始まっていた場合は `resume` と同じバージョンを指定する。
3. Git タグだけ残っていれば元の SHA を再検証してビルドする。正式イメージがあれば、OCI の source・version・revision と Digest を検証してそのまま使用する。Release が既にあれば公開状態と本文の配布情報を照合する。可変タグだけ更新に失敗した場合は、正式イメージの manifest で不足分を修復する。新しい正式 Release がある場合、古い Release の復旧では可変タグと GitHub の Latest 表示を動かさない。
4. SHA の不一致、孤立した SHA タグ、Release 本文の不整合、未知のエイリアス、API エラーで止まった場合は、管理者が Job Summary と GitHub・GHCR の状態を確認する。元の実行 SHA、パッケージ権限、旧 Workflow の停止状態、競合する外部操作を調べる。公開済みのタグや Release は自動削除しない。

## FEATO インフラでのデプロイと切り戻し

現行の `FEATO-org/it-infrastructure-server` は `deploys/app/compose.yml` の `sfs-discord-server` で `ghcr.io/feato-org/support-feato-system:main` を参照している。初回正式公開を確認した後、インフラ側の後続 PR で GitHub Release 記載の参照を使用する。

```yaml
image: ghcr.io/feato-org/support-feato-system:YYYY-MM-DD.N@sha256:<release-digest>
```

`deploys/versions.md` と関連する運用記録に、バージョンと Digest を記録する。Swarm の replicas、リソース制限、`start-first`、`failure_action: rollback` と既存の環境変数は保持する。運用承認後、インフラ側の `deploys/README.md` に従って `script/deploy_swarm.sh` を実行する。このスクリプトは infra、app、system の順でスタックを更新する。`docker service ps app_sfs-discord-server` で更新状態を確認し、サービスのログと Bot の状態を照合する。切り戻す場合はインフラ側の Compose を直前の検証済みバージョンと Digest に戻し、同じ手順で再適用する。初回切り替え前のイメージに戻す必要がある場合は、初回導入時に控えた旧 `:main` の Digest を使用する。

## 参照

- [GitHub Releases API と Notes 生成](https://docs.github.com/en/rest/releases/releases#generate-release-notes-content-for-a-release)
- [GitHub Actions の同時実行制御](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)
- [Docker Buildx の OCI exporter](https://docs.docker.com/build/exporters/oci-docker/)
- [Google Distroless のサポート対象](https://github.com/GoogleContainerTools/distroless#what-images-are-available)。現行の `static-debian11` は取得可能だがサポート対象一覧から外れている。更新は別途扱う。
