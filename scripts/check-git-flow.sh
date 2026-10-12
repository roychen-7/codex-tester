#!/bin/sh
set -eu

valid_branch() {
  printf '%s\n' "$1" | LC_ALL=C grep -Eq '^(main|develop|(feature|release|hotfix)/[a-z0-9]+([.-][a-z0-9]+)*)$'
}

case "${1:-}" in
  branch)
    if ! valid_branch "${2:-}"; then
      echo '分支名必须为 main、develop、feature/<主题>、release/<版本> 或 hotfix/<主题>（小写字母、数字、点和连字符）。' >&2
      exit 1
    fi
    ;;
  pr)
    source_branch=${2:-}
    target_branch=${3:-}
    valid_branch "$source_branch" && valid_branch "$target_branch" || {
      echo 'PR 包含不符合 Git Flow 约定的分支名。' >&2
      exit 1
    }
    case "$source_branch:$target_branch" in
      feature/*:develop|release/*:main|release/*:develop|hotfix/*:main|hotfix/*:develop|main:develop) ;;
      *)
        echo "不允许的 PR 流向：$source_branch -> $target_branch。功能进入 develop，发布和热修复进入 main 并回合 develop。" >&2
        exit 1
        ;;
    esac
    ;;
  *) echo '用法：check-git-flow.sh branch <分支> | pr <来源> <目标>' >&2; exit 2 ;;
esac
