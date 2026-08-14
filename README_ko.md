# agentgit

**agentgit**은 Git을 이해하는 command 작업 공간입니다. 하단 한 줄 command bar가 있는 프로젝트 터미널을 열고, 필요할 때 Git 히스토리와 로컬 에이전트 트랜스크립트를 탐색할 수 있습니다.

[English Docs (README.md)](./README.md)

## 빠른 시작

1. **설치**: `agentgit` 바이너리를 `PATH` 경로에 추가합니다.
2. **사용**: 저장소에서 `agentgit`을 실행하면 tmux 같은 래퍼 안에서 기본 shell이 시작됩니다.
   ```sh
   agentgit
   ```

## 주요 명령어

| 명령어 | 설명 |
| :--- | :--- |
| `agentgit` | 기본 shell을 래퍼 안에서 실행합니다. |
| `agentgit -- codex` | 특정 명령을 래퍼 안에서 실행합니다. |
| `agentgit browse` | 현재 디렉토리의 히스토리 브라우저를 엽니다. |
| `agentgit browse [path]` | 특정 저장소, 폴더 또는 파일의 히스토리를 확인합니다. |
| `agentgit browse --limit 50` | 브라우저에 표시할 커밋 개수를 제한합니다. |

## 터미널 래퍼

`agentgit`은 현재 저장소에서 shell 또는 지정한 명령을 PTY로 실행합니다. command terminal은 하단 status line 한 줄을 제외한 터미널 전체를 사용하고, 마지막 줄은 `agentgit`이 커밋 브라우저 진입 표시용으로 유지합니다.

- **커밋 브라우저 열기**: `Ctrl+G`
- **커밋 브라우저에서 에이전트로 복귀**: `Esc`

명령을 지정하지 않으면 `AGENTGIT_AGENT` 환경 변수를 먼저 사용하고, 없으면 `$SHELL`을 엽니다. 특정 명령으로 시작하려면 `agentgit -- claude`처럼 `--` 뒤에 명령을 넘기면 됩니다.

## TUI 조작법

상단 컨텍스트 바에는 base path, Git branch, HEAD, 커밋 수, dirty 파일 수가 표시됩니다.

- **이동**: `Up`/`Down`
- **커밋/디렉토리 뷰 전환**: `Tab`
- **최신 커밋 선택**: `s` 진입, `Space` 선택, `x` 삭제, `m` 병합, `y` 확인
- **파일 검색**: `Ctrl+P` 입력 후 fuzzy 검색하고 `Enter`로 Directory 뷰에서 파일 위치 열기
- **최근 파일**: `Ctrl+E`로 최근에 열었던 파일을 필터링하고 `Enter`로 다시 열기
- **디렉토리 폴더**: `Enter`/`Right`로 폴더 접기/펼치기 및 파일 열기
- **상세 보기**: `Right` 또는 `Enter` (커밋 → 파일 목록 → Diff)
- **이미지 열기**: 파일 목록에서 이미지 파일 선택 후 `Enter`
- **뒤로 가기**: `Left` 또는 `Backspace`
- **Request 드로어**: `v`로 하단 드로어 열기/닫기, `Enter`로 전체 request 상세 보기
- **Push**: `g` 다음 `p`로 `git push` 실행
- **머지된 브랜치 삭제**: `g` 다음 `b` 다음 `d`로 `git branch --merged` 기준 로컬 브랜치 삭제. 현재 브랜치와 `main`/`master`/`develop`/`dev`는 제외
- **새로고침**: `r`
- **화면 전환**: `m` (Unified/Split diff)
- **줄 번호 전환**: `l` (Diff/전체 파일)
- **긴 줄 전체 보기**: `w` (Diff/전체 파일/Request에서 줄바꿈 전환)
- **다음/이전 변경점**: `n`/`p`
- **단축키 도움말**: `?`
- **종료**: `Ctrl+C`

## 동작 방식

- **기본은 수동 탐색**: 탐색 중에는 훅 설치, 전역 설정 변경, Git hook 변경, 커밋 생성, reset, request 기록 쓰기를 하지 않습니다.
- **Select Mode**: 명시적으로 선택한 최신 커밋은 작업 트리가 깨끗하고 `HEAD`부터 끊김 없이 이어진 구간일 때만 삭제하거나 병합할 수 있습니다. 삭제는 `git reset --hard`를 사용하고, 병합은 선택 커밋을 squash합니다.
- **트랜스크립트**: request는 Claude, Codex, Gemini의 로컬 트랜스크립트 파일에서 스캔합니다.
- **사실만 표시**: timestamp 근사 같은 방식으로 request와 commit을 연결하지 않습니다. Git 정보는 Git에서, request 정보는 트랜스크립트 필드에서 가져옵니다.
- **로컬 DB**: SQLite 스키마는 `~/.local/share/agentgit/agentgit.sqlite3`에 초기화될 수 있지만, request 브라우징은 트랜스크립트 스캔이 기준입니다.

## 설치 방법

운영체제와 CPU 아키텍처에 맞는 바이너리를 다운로드한 후 설치하세요.

```sh
# macOS (Apple Silicon) 예시
mkdir -p ~/.local/bin
install -m 0755 dist/agentgit_darwin_arm64 ~/.local/bin/agentgit

# PATH 추가 (zsh)
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

## 빌드 방법

```sh
# 로컬 바이너리 빌드
go build -o dist/agentgit ./cmd/agentgit

# 배포용 바이너리 빌드
make release
```
