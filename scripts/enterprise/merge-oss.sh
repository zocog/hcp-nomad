#!/usr/bin/env bash

set -o errexit
set -o nounset

usage() {
    cat <<'EOF'
  usage: merge-oss.sh [OSS_BRANCH] [ENT_BRANCH] [TMP_BRANCH]

merge-oss automates propagating OSS changes into Enterprise, and resolves common
conflict resources. On success, it creates a temporary branch that can be used
to open a PR for merger

Defaults to merging oss/main to oss/
EOF
}

if [ $# -gt 3 ] || [[ $# -eq 1 && "${1}" == '--help' ]]
then
    usage
    exit 1
fi

origin_branch="${1:-main}"
dest_branch="${2:-main}"
tmp_branch="${3:-}"

if [ -z "${tmp_branch}" ]; then
    tmp_branch="oss-merge-${origin_branch}-$(date -u +%Y%m%d%H%M%S)"
fi

git pull origin "${dest_branch}"

# Merge OSS main branch to Enterprise merge branch
if ! git remote get-url oss 1>/dev/null 2>/dev/null; then
    git remote add oss https://github.com/hashicorp/nomad.git
fi

git fetch oss "${origin_branch}"

git checkout -b "${tmp_branch}"
git reset --hard "origin/${dest_branch}"

latest_oss_commit="$(git rev-parse "oss/${origin_branch}")"
message="Merge Nomad OSS branch '${origin_branch}' at commit ${latest_oss_commit}"

if ! git merge -m "$message" "oss/${origin_branch}"; then
    # try to merge common conflicting files
    git status
    git checkout --theirs CHANGELOG.md
    git checkout --theirs version/version.go
    git checkout --theirs command/agent/bindata_assetfs.go
    git checkout --theirs .circleci/config.yml
    git checkout --ours   vendor/modules.txt
    git checkout --ours   go.sum
    make sync

    # Regenerate enterprise CircleCI config to apply changes from OSS merge

    make -C .circleci config.yml

    git add CHANGELOG.md version/version.go command/agent/bindata_assetfs.go \
        go.sum vendor/modules.txt .circleci/config.yml

    # attempt merging again
    if ! git commit -m "$message"; then
        echo "failed to auto merge" >&2
        exit 1
    fi
fi

if [[ -z "${CI:-}" ]]; then
    echo
    echo "push branch and open a PR"
    echo "   git push origin ${tmp_branch}"
fi

