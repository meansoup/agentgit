# agentgit

**agentgit**은 AI 코딩 에이전트의 요청(Request)과 Git 커밋을 연결해주는 도구입니다. 기존처럼 에이전트(`codex`, `gemini`)를 사용하면, `agentgit`이 백그라운드에서 요청 내용을 기록하고 생성된 커밋을 TUI 브라우저로 보여줍니다.

[English Docs (README.md)](./README.md)

## 빠른 시작

1. **설치**: `agentgit` 바이너리를 `PATH` 경로에 추가합니다.
2. **설정**: 사용할 AI 에이전트와 연결합니다.
   ```sh
   agentgit setup codex
   # 또는
   agentgit setup gemini
   ```
3. **사용**: 에이전트(예: `codex`)를 평소처럼 사용한 뒤, 연결된 히스토리를 확인합니다.
   ```sh
   agentgit
   ```

## 주요 명령어

| 명령어 | 설명 |
| :--- | :--- |
| `agentgit` | 현재 디렉토리의 히스토리 브라우저를 엽니다. |
| `agentgit [path]` | 특정 저장소, 폴더 또는 파일의 히스토리를 확인합니다. |
| `agentgit setup [agent]` | `codex` 또는 `gemini`를 위한 훅(hook)을 설치합니다. |
| `agentgit --limit 50` | 표시할 커밋 개수를 제한합니다. |

## TUI 조작법

상단 컨텍스트 바에는 base path, Git branch, HEAD, 커밋 수, dirty 파일 수가 표시됩니다.

- **이동**: `Up`/`Down`
- **커밋/디렉토리 뷰 전환**: `Tab`
- **최신 커밋 선택**: `s` 진입, `Space` 선택, `x` 제거, `m` 병합, `y` 확인
- **상세 보기**: `Right` 또는 `Enter` (커밋 → 파일 목록 → Diff)
- **이미지 열기**: 파일 목록에서 이미지 파일 선택 후 `Enter`
- **뒤로 가기**: `Left` 또는 `Backspace`
- **요청 상세 보기**: `v`
- **새로고침**: `r`
- **화면 전환**: `m` (Unified/Split diff)
- **다음/이전 변경점**: `n`/`p`
- **단축키 도움말**: `?`
- **종료**: `q`

## 동작 방식

- **훅(Hooks)**: `agentgit setup`은 에이전트 이벤트가 발생할 때 실행되는 라이프사이클 훅을 설치합니다.
- **커밋 연결**: 에이전트 요청 중 생성된 커밋을 해당 요청에 연결하며, `agentgit`이 커밋을 만들거나 커밋 메시지를 바꾸지 않습니다.
- **Select Mode**: 작업 트리가 깨끗하고 `HEAD`부터 끊김 없이 이어지는 최신 커밋 구간만 제거하거나 병합할 수 있습니다. 제거는 `git reset --hard`를 사용하고, 병합은 선택 커밋을 squash한 뒤 해당 request 링크를 새 커밋으로 옮깁니다.
- **로컬 DB**: 모든 메타데이터는 `~/.local/share/agentgit/agentgit.sqlite3`에 저장됩니다.
- **투명성**: 기존 작업 방식은 그대로 유지됩니다. `agentgit`은 보이지 않는 곳에서 조용히 작동합니다.

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
