#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
version_script="$script_dir/release-version.sh"
test_dir=$(mktemp -d)
trap 'rm -rf -- "$test_dir"' EXIT

# Keep these tests independent of the caller's Git configuration and repository.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
export GIT_AUTHOR_NAME='Release Test' GIT_AUTHOR_EMAIL='release@example.invalid'
export GIT_COMMITTER_NAME='Release Test' GIT_COMMITTER_EMAIL='release@example.invalid'

git init --quiet "$test_dir/repo"
cd -- "$test_dir/repo"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_equal() {
    [[ "$1" == "$2" ]] || fail "${3}: expected '$1', got '$2'"
}

create_commit() {
    # Author time intentionally differs: release versions must use committer time.
    GIT_AUTHOR_DATE='2001-02-03T04:05:06Z' GIT_COMMITTER_DATE="$2" \
        git -c commit.gpgsign=false -c core.hooksPath=/dev/null \
        commit --allow-empty --quiet -m "$1"
    git rev-parse HEAD
}

printf 'original\n' > sample.txt
git add sample.txt
first_commit=$(create_commit first '2026-01-02T00:03:04+09:00')
expected_first="v2026.1.1-150304-g${first_commit:0:12}"

for timezone in UTC0 HST10 JST-9; do
    actual=$(TZ="$timezone" "$version_script")
    assert_equal "$expected_first" "$actual" "UTC date in $timezone"
done
assert_equal "$expected_first" "$("$version_script")" 'repeated invocation'

second_commit=$(create_commit second '2026-01-02T00:03:04+09:00')
second_version=$("$version_script")
assert_equal "v2026.1.1-150304-g${second_commit:0:12}" "$second_version" 'same timestamp'
[[ "$second_version" != "$expected_first" ]] || fail 'same-time commits share a version'

third_commit=$(create_commit third '2026-10-11T19:59:52Z')
assert_equal "v2026.10.11-195952-g${third_commit:0:12}" "$("$version_script")" 'two-digit date'
assert_equal "$expected_first" "$("$version_script" "$first_commit")" 'explicit commit SHA'
assert_equal "$expected_first" "$("$version_script" HEAD~2)" 'explicit relative revision'

for invalid_revision in missing-revision --not-a-revision; do
    if "$version_script" "$invalid_revision" > "$test_dir/stdout" 2> "$test_dir/stderr"; then
        fail "accepted invalid revision: $invalid_revision"
    fi
    [[ ! -s "$test_dir/stdout" ]] || fail 'invalid revision emitted a version'
done
if "$version_script" HEAD HEAD > "$test_dir/stdout" 2> "$test_dir/stderr"; then
    fail 'accepted extra arguments'
fi

# Preserve staged edits, unstaged edits, untracked files, refs, and the index.
printf 'staged\n' > sample.txt
git add sample.txt
printf 'unstaged\n' >> sample.txt
printf 'untracked\n' > extra.txt
refs_before=$(git show-ref)
status_before=$(git status --porcelain)
index_before=$(git hash-object .git/index)
worktree_before=$(git hash-object sample.txt extra.txt)
"$version_script" > "$test_dir/version"
assert_equal "$refs_before" "$(git show-ref)" 'refs remain unchanged'
assert_equal "$status_before" "$(git status --porcelain)" 'worktree status remains unchanged'
assert_equal "$index_before" "$(git hash-object .git/index)" 'index remains unchanged'
assert_equal "$worktree_before" "$(git hash-object sample.txt extra.txt)" 'files remain unchanged'
[[ $(wc -l < "$test_dir/version") -eq 1 ]] || fail 'version output is not one line'
[[ $(cat "$test_dir/version") =~ ^v[0-9]{4}\.[1-9][0-9]*\.[1-9][0-9]*-[0-9]{6}-g[0-9a-f]{12}$ ]] \
    || fail 'version is not in the expected SemVer-compatible format'

printf 'release-version tests passed\n'
