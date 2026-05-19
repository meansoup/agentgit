# agentgit

English docs: [README.md](./README.md)

`README.md`와 항상 같은 내용을 유지하세요.

`agentgit`는 AI 에이전트 요청을 로컬 SQLite 데이터베이스에 저장하고, Git
commit과 hook으로 연결하며, commit/file/diff를 탐색하는 TUI를 제공합니다.

## 빠른 시작

```sh
# release binary를 PATH에 먼저 설치한다.
agentgit setup

cd /path/to/project
agentgit codex start --model gpt-5 --message "로그인 검증 구현"
# Codex를 실행하고 파일을 수정한다.
agentgit codex commit -m "로그인 검증 구현"
agentgit codex finish

agentgit log
```

`agentgit setup`은 PC당 한 번만 하면 되는 초기 설정입니다. 로컬 데이터베이스를
초기화하고 전역 Git `post-commit` hook을 설치하므로, 같은 머신의 어떤 Git
repository에서도 사용할 수 있습니다.

## 사용 흐름

### 1. 에이전트 요청 시작

```sh
agentgit codex start --model gpt-5 --message "요청 내용을 설명"
```

Claude와 Gemini도 같은 흐름을 사용합니다.

```sh
agentgit claude start --model claude-sonnet-4.5 --message "요청 내용을 설명"
agentgit gemini start --model gemini-2.5-pro --message "요청 내용을 설명"
```

`start`는 요청 전에 이미 변경되어 있던 파일을 snapshot으로 저장합니다. 이 파일들은
기본적으로 요청 commit에서 제외됩니다.

만약 AI 요청이 `start` 전에 이미 파일을 바꿨다면 다음 옵션을 사용합니다.

```sh
agentgit codex start --model gpt-5 --message "요청 내용을 설명" --include-current
```

### 2. 요청 소유 변경만 commit

에이전트가 코드를 수정한 뒤에는 다음처럼 commit합니다.

```sh
agentgit codex commit -m "요청 반영"
```

이 명령은 `start` 이후에 새로 dirty가 된 파일만 stage하고 commit합니다. Git hook이
새 commit을 로컬 SQLite 데이터베이스의 active request와 연결합니다.

다른 provider도 같은 방식입니다.

```sh
agentgit claude commit -m "요청 반영"
agentgit gemini commit -m "요청 반영"
```

### 3. 요청 종료

```sh
agentgit codex finish
```

provider별 변형도 가능합니다.

```sh
agentgit claude finish
agentgit gemini finish
```

### 4. request가 연결된 commit 보기

```sh
agentgit log
```

자주 쓰는 옵션:

```sh
agentgit log --limit 100
```

TUI 키:

- `Up` / `Down` 또는 `k` / `j`: 커서 이동
- `Right` / `Enter` 또는 `l`: commit -> file -> diff
- `Left` / `Backspace` 또는 `h`: diff -> file -> commit
- `m`: unified / split diff 전환
- `q`: 종료

TTY가 없으면 `agentgit log`는 TUI 대신 색상이 있는 정적 목록을 출력합니다.

## 명령어

```sh
agentgit setup
agentgit setup-local
agentgit log --limit 500
agentgit version

agentgit codex start --model <model> --message <message>
agentgit codex commit -m <commit-message>
agentgit codex finish

agentgit claude start --model <model> --message <message>
agentgit claude commit -m <commit-message>
agentgit claude finish

agentgit gemini start --model <model> --message <message>
agentgit gemini commit -m <commit-message>
agentgit gemini finish
```

generic provider 형태:

```sh
agentgit request --provider codex start --model gpt-5 --message "request"
agentgit request --provider claude commit -m "commit message"
agentgit request --provider gemini finish
```

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

`export PATH=...`는 현재 셸 세션에만 적용됩니다. 새 터미널이나 재부팅 후에도
계속 쓰려면 shell startup file에 넣어야 합니다.

source checkout에서 개발할 때는 `bin/agentgit` wrapper가 `go run`으로 Go 명령을
실행합니다. 배포 사용자는 컴파일된 binary를 설치해서 쓰는 편이 낫습니다.

## 셋업

```sh
agentgit setup
```

이 명령은 데이터베이스를 초기화하고 Git 전역 `core.hooksPath`를
`~/.config/agentgit/hooks`로 설정합니다. 다른 global hooks path가 이미 있으면
agentgit은 자신의 `post-commit` 이후에 기존 hook도 이어서 호출합니다.

## repository 단위 셋업

```sh
agentgit setup-local
```

이 명령은 PC 전체가 아니라 현재 repository에만 hook을 설치합니다. 기본 데이터베이스
경로는 다음과 같습니다.

```text
~/.local/share/agentgit/agentgit.sqlite3
```

환경 변수로 덮어쓸 수 있습니다.

```sh
AGENTGIT_DB=/path/to/agentgit.sqlite3
```

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
go run ./cmd/agentgit --help
```
