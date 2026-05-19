from __future__ import annotations

import curses
import sys
from dataclasses import dataclass
from pathlib import Path

from . import db
from .git import Commit, changed_files, commits, unified_diff


@dataclass
class ViewState:
    mode: str = "commits"
    commit_idx: int = 0
    file_idx: int = 0
    scroll: int = 0
    diff_mode: str = "unified"


class App:
    def __init__(self, root: Path, limit: int) -> None:
        self.root = root
        self.commits = commits(root, limit)
        self.links = db.requests_by_commit(root)
        self.state = ViewState()
        self.files: list[str] = []
        self.diff_lines: list[str] = []

    def run(self, stdscr: curses.window) -> None:
        curses.curs_set(0)
        curses.use_default_colors()
        curses.init_pair(1, curses.COLOR_CYAN, -1)
        curses.init_pair(2, curses.COLOR_YELLOW, -1)
        curses.init_pair(3, curses.COLOR_GREEN, -1)
        curses.init_pair(4, curses.COLOR_MAGENTA, -1)
        curses.init_pair(5, curses.COLOR_RED, -1)
        curses.init_pair(6, curses.COLOR_BLUE, -1)
        stdscr.keypad(True)
        while True:
            self.draw(stdscr)
            key = stdscr.getch()
            if key in (ord("q"), 27):
                return
            self.handle_key(key)

    def handle_key(self, key: int) -> None:
        if key == curses.KEY_UP:
            self.move(-1)
        elif key == curses.KEY_DOWN:
            self.move(1)
        elif key == curses.KEY_RIGHT:
            self.enter()
        elif key == curses.KEY_LEFT:
            self.back()
        elif key == ord("m"):
            self.state.diff_mode = "split" if self.state.diff_mode == "unified" else "unified"

    def move(self, delta: int) -> None:
        if self.state.mode == "commits":
            self.state.commit_idx = clamp(self.state.commit_idx + delta, 0, len(self.commits) - 1)
        elif self.state.mode == "files":
            self.state.file_idx = clamp(self.state.file_idx + delta, 0, len(self.files) - 1)
        else:
            self.state.scroll = max(0, self.state.scroll + delta)

    def enter(self) -> None:
        if self.state.mode == "commits" and self.commits:
            commit_hash = self.commits[self.state.commit_idx].hash
            self.files = changed_files(self.root, commit_hash)
            self.state.file_idx = 0
            self.state.mode = "files"
        elif self.state.mode == "files" and self.files:
            commit_hash = self.commits[self.state.commit_idx].hash
            self.diff_lines = unified_diff(self.root, commit_hash, self.files[self.state.file_idx])
            self.state.scroll = 0
            self.state.mode = "diff"

    def back(self) -> None:
        if self.state.mode == "diff":
            self.state.mode = "files"
        elif self.state.mode == "files":
            self.state.mode = "commits"

    def draw(self, stdscr: curses.window) -> None:
        stdscr.erase()
        height, width = stdscr.getmaxyx()
        title = f"agentgit {self.state.mode}  m:{self.state.diff_mode}  q:quit"
        addstr(stdscr, 0, 0, title[: width - 1], curses.A_BOLD)
        if self.state.mode == "commits":
            self.draw_commits(stdscr, height, width)
        elif self.state.mode == "files":
            self.draw_files(stdscr, height, width)
        else:
            self.draw_diff(stdscr, height, width)
        stdscr.refresh()

    def draw_commits(self, stdscr: curses.window, height: int, width: int) -> None:
        row = 2
        start = max(0, self.state.commit_idx - height // 3)
        for idx, commit in enumerate(self.commits[start:], start):
            if row >= height:
                break
            selected = curses.A_REVERSE if idx == self.state.commit_idx else curses.A_NORMAL
            addstr(stdscr, row, 0, commit.short_hash, curses.color_pair(1) | selected)
            addstr(stdscr, row, 10, commit.date, selected)
            addstr(stdscr, row, 23, commit.subject[: max(0, width - 24)], selected)
            row += 1
            for request in self.links.get(commit.hash, []):
                if row >= height:
                    break
                addstr(stdscr, row, 2, "└─ ● ", curses.color_pair(4))
                addstr(stdscr, row, 7, f"[{request.provider} {request.model}] ", curses.color_pair(2))
                addstr(stdscr, row, 7 + len(f"[{request.provider} {request.model}] "), request.message[: max(0, width - 30)], curses.color_pair(3))
                row += 1

    def draw_files(self, stdscr: curses.window, height: int, width: int) -> None:
        commit = self.commits[self.state.commit_idx]
        addstr(stdscr, 1, 0, f"{commit.short_hash} {commit.subject}"[: width - 1], curses.color_pair(1))
        start = max(0, self.state.file_idx - height // 3)
        for row, idx in enumerate(range(start, len(self.files)), 3):
            if row >= height:
                break
            selected = curses.A_REVERSE if idx == self.state.file_idx else curses.A_NORMAL
            addstr(stdscr, row, 0, self.files[idx][: width - 1], selected | curses.color_pair(6))

    def draw_diff(self, stdscr: curses.window, height: int, width: int) -> None:
        commit = self.commits[self.state.commit_idx]
        path = self.files[self.state.file_idx]
        addstr(stdscr, 1, 0, f"{commit.short_hash} {path}"[: width - 1], curses.color_pair(1))
        lines = split_diff(self.diff_lines, width) if self.state.diff_mode == "split" else self.diff_lines
        for row, line in enumerate(lines[self.state.scroll :], 3):
            if row >= height:
                break
            color = curses.A_NORMAL
            if line.startswith("+") and not line.startswith("+++"):
                color = curses.color_pair(3)
            elif line.startswith("-") and not line.startswith("---"):
                color = curses.color_pair(5)
            elif line.startswith("@@"):
                color = curses.color_pair(4)
            addstr(stdscr, row, 0, line[: width - 1], color)


def clamp(value: int, lower: int, upper: int) -> int:
    if upper < lower:
        return lower
    return max(lower, min(upper, value))


def addstr(stdscr: curses.window, y: int, x: int, text: str, attr: int = 0) -> None:
    try:
        stdscr.addstr(y, x, text, attr)
    except curses.error:
        pass


def split_diff(lines: list[str], width: int) -> list[str]:
    left_width = max(20, (width - 3) // 2)
    right_width = max(20, width - left_width - 3)
    output: list[str] = []
    pending_remove: str | None = None
    for line in lines:
        if line.startswith("---") or line.startswith("+++") or line.startswith("@@"):
            if pending_remove is not None:
                output.append(format_split(pending_remove, "", left_width, right_width))
                pending_remove = None
            output.append(line)
        elif line.startswith("-"):
            if pending_remove is not None:
                output.append(format_split(pending_remove, "", left_width, right_width))
            pending_remove = line[1:]
        elif line.startswith("+"):
            if pending_remove is not None:
                output.append(format_split(pending_remove, line[1:], left_width, right_width))
                pending_remove = None
            else:
                output.append(format_split("", line[1:], left_width, right_width))
        else:
            if pending_remove is not None:
                output.append(format_split(pending_remove, "", left_width, right_width))
                pending_remove = None
            text = line[1:] if line.startswith(" ") else line
            output.append(format_split(text, text, left_width, right_width))
    if pending_remove is not None:
        output.append(format_split(pending_remove, "", left_width, right_width))
    return output


def format_split(left: str, right: str, left_width: int, right_width: int) -> str:
    return f"{left[:left_width]:<{left_width}} │ {right[:right_width]:<{right_width}}"


def print_static(root: Path, limit: int) -> None:
    all_commits = commits(root, limit)
    links = db.requests_by_commit(root)
    for commit in all_commits:
        print(f"\033[36m{commit.short_hash}\033[0m {commit.date}  {commit.subject}")
        for request in links.get(commit.hash, []):
            print(
                "\033[35m└─ ●\033[0m "
                f"\033[33m[{request.provider} {request.model}]\033[0m "
                f"\033[32m{request.message}\033[0m"
            )


def run(root: Path, limit: int = 500) -> None:
    if not sys.stdin.isatty() or not sys.stdout.isatty():
        print_static(root, limit)
        return
    curses.wrapper(App(root, limit).run)
