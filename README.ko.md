# agentgit

English docs: [README.md](./README.md)

`README.md`와 항상 같은 내용을 유지하세요.

`agentgit`은 AI agent CLI 사용 방식을 바꾸지 않고 agent request와 Git commit을
연결합니다. 설정 후에는 기존처럼 `codex`를 그대로 사용합니다. `agentgit`은 Codex
lifecycle hook 이벤트를 받아 request를 로컬 SQLite 데이터베이스에 기록하고,
request가 파일을 변경했다면 request 단위 commit을 만들고, request와 commit이
연결된 Git history를 TUI로 보여줍니다.

## 명령어

사용자가 직접 쓰는 명령어는 두 개입니다.

```sh
agentgit
agentgit setup codex
```

`agentgit [path]`는 현재 path 또는 지정 path에 대한 request-linked commit을
보여줍니다.

`agentgit setup codex`는 이 PC에 Codex lifecycle hook을 한 번 설치합니다.

내부 hook 명령:

```sh
agentgit hook codex
```

이 명령은 Codex hook 설정에 기록되는 내부용 명령이며, 직접 실행할 필요가 없습니다.

## 빠른 시작

```sh
# release binary를 PATH에 먼저 설치한다.
agentgit setup codex

cd /path/to/project
codex
# Codex는 평소처럼 사용한다. Git repository에서 request가 파일을 변경하면
# agentgit이 request를 기록하고 request 단위 commit을 만든다.

agentgit
```

Codex가 `/hooks`에서 새 hook을 검토하고 trust하라고 요청할 수 있습니다. non-managed
Codex hook에서는 정상적인 흐름입니다.

## 동작 방식

`agentgit setup codex`는 hook 설정을 다음 위치에 씁니다.

```text
~/.codex/hooks.json
```

Codex hook 흐름:

- `UserPromptSubmit`: request message, agent name, model, session id, turn id,
  현재 Git root, dirty-file baseline, 현재 `HEAD`를 기록합니다.
- `Stop`: 현재 working tree를 baseline과 비교해서 해당 request가 변경한 파일만
  commit하고, 생성된 commit을 기록된 request에 연결합니다.
- Codex 또는 사용자가 request 중 이미 commit을 만들었다면, baseline `HEAD` 이후에
  생성된 commit을 request에 연결합니다.

데이터베이스는 로컬이며 특정 CLI에 종속되지 않는 용어를 사용합니다.

```text
~/.local/share/agentgit/agentgit.sqlite3
```

환경 변수로 덮어쓸 수 있습니다.

```sh
AGENTGIT_DB=/path/to/agentgit.sqlite3
```

## request-linked commit 보기

현재 path:

```sh
agentgit
```

특정 repository, directory, file path:

```sh
agentgit /path/to/project
agentgit /path/to/project/file.go
```

commit 수 제한:

```sh
agentgit --limit 100
agentgit --limit 100 /path/to/project
```

TUI 키:

- `Up` / `Down` 또는 `k` / `j`: 커서 이동
- `Right` / `Enter` 또는 `l`: commit -> file -> diff
- `Left` / `Backspace` 또는 `h`: diff -> file -> commit
- `m`: unified / split diff 전환
- `q`: 종료

TTY가 없으면 `agentgit`은 TUI 대신 색상이 있는 정적 목록을 출력합니다.

예시:

```text
7cbbb0c8 04-06 15:16  commit message
└─ ● [codex gpt-5] request message
12345678 04-06 15:16  another commit
```

## Provider 상태

현재 지원:

- `agentgit setup codex`

추가 예정:

- `agentgit setup claude`
- `agentgit setup gemini`

데이터베이스 schema는 `agent_name`, `model`, `session_id`, `turn_id`,
`request_commits`처럼 일반적인 agent 용어를 사용하므로 Codex 전용 용어에 묶이지
않습니다.

## 설치

배포본은 OS와 CPU architecture에 맞는 binary를 PATH에 두고 사용합니다.

### macOS

Apple Silicon:

```sh
install -m 0755 dist/agentgit_<version>_darwin_arm64 /usr/local/bin/agentgit
```

Intel Mac:

```sh
install -m 0755 dist/agentgit_<version>_darwin_amd64 /usr/local/bin/agentgit
```

`/usr/local/bin`에 쓸 수 없으면 사용자 디렉터리에 설치합니다.

```sh
mkdir -p "$HOME/.local/bin"
install -m 0755 dist/agentgit_<version>_darwin_arm64 "$HOME/.local/bin/agentgit"
```

macOS `zsh`에서 영구 PATH 설정:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

macOS `bash`에서 영구 PATH 설정:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bash_profile
source ~/.bash_profile
```

### Ubuntu

x86_64:

```sh
install -m 0755 dist/agentgit_<version>_linux_amd64 /usr/local/bin/agentgit
```

ARM64:

```sh
install -m 0755 dist/agentgit_<version>_linux_arm64 /usr/local/bin/agentgit
```

`/usr/local/bin`에 쓸 수 없으면 사용자 디렉터리에 설치합니다.

```sh
mkdir -p "$HOME/.local/bin"
install -m 0755 dist/agentgit_<version>_linux_amd64 "$HOME/.local/bin/agentgit"
```

Ubuntu `bash`에서 영구 PATH 설정:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

Ubuntu `zsh`에서 영구 PATH 설정:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

`export PATH=...`는 현재 셸 세션에만 적용됩니다. 새 터미널이나 재부팅 후에도 계속
쓰려면 shell startup file에 넣어야 합니다.

source checkout에서 개발할 때는 `bin/agentgit` wrapper가 `go run`으로 Go 명령을
실행합니다. 배포 사용자는 컴파일된 binary를 설치해서 쓰는 편이 낫습니다.

## 빌드

로컬 binary 빌드:

```sh
go build -o dist/agentgit ./cmd/agentgit
```

release binary 빌드:

```sh
make release
```

`dist/`에 생성되는 파일:

- `agentgit_<version>_darwin_amd64`
- `agentgit_<version>_darwin_arm64`
- `agentgit_<version>_linux_amd64`
- `agentgit_<version>_linux_arm64`

개발용 실행:

```sh
go run ./cmd/agentgit -- --help
```
