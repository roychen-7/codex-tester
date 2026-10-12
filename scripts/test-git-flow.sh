#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
assert_result() {
  expected=$1
  shift
  actual=0
  sh "$root/scripts/check-git-flow.sh" "$@" >/dev/null 2>&1 || actual=$?
  if [ "$actual" -ne "$expected" ]; then
    echo "FAIL: $* (expected $expected, got $actual)" >&2
    exit 1
  fi
}
for branch in main develop feature/task-filter feature/fix-task release/1.2.0 hotfix/task-error; do
  assert_result 0 branch "$branch"
done
for branch in '' master feat-task codex/task feature/ feature/Bad feature/a/b 'feature/task;echo'; do
  assert_result 1 branch "$branch"
done
assert_result 0 pr feature/task develop
assert_result 0 pr release/1.0.0 main
assert_result 0 pr release/1.0.0 develop
assert_result 0 pr hotfix/task main
assert_result 0 pr hotfix/task develop
assert_result 0 pr main develop
assert_result 1 pr feature/task main
assert_result 1 pr develop main
assert_result 1 pr main main
assert_result 1 pr feature/task release/1.0.0
assert_result 1 pr feat-task develop
assert_result 1 pr release/ main
assert_result 2 unknown
echo 'Git Flow branch and PR checks passed.'
