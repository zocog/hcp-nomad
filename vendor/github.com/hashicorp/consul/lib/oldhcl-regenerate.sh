#!/usr/bin/env bash

# This script will rebuild the 'oldhcl' package from its original contents
# and then apply patches as needed for the modified behavior.

unset CDPATH

set -euo pipefail

cd "$(dirname "$0")"

readonly basedir="$PWD"
readonly gitdir="${basedir}/hcl-regen-git"
readonly outdir="${basedir}/hcl-regen-output"
readonly finaldir="${basedir}/oldhcl"

die() {
    echo "ERROR: $1" >&2
    exit 1
}

# this is
rm -rf "${outdir}"
mkdir -p "${outdir}"

reclone() {
    (
        cd "${basedir}"
        if [[ -e hcl-regen-git ]]; then
            rm -rf hcl-regen-git
        fi
        git clone git@github.com:hashicorp/hcl.git hcl-regen-git || {
            die "Error cloning HCL repo"
        }
    )
}

# ensure we have the git clone checked out
if [[ -e "${gitdir}" ]]; then
    if [[ ! -d "${gitdir}/.git" ]]; then
        reclone
    fi
else
    reclone
fi

readonly sha1_root="23c074d0eceb2b8a5bfdbb271ab780cde70f05a8"
readonly sha1_children="d8c773c4cba11b11539e3d45f93daeaa5dcf1fa1"

# The files in the root directory come from one SHA1, while children
# come from a different SHA1. Why? Who knows.
(
    cd "${gitdir}"
    git reset --hard "${sha1_root}"
    cp LICENSE "${outdir}/LICENSE"
    cp -a "test-fixtures" "${outdir}/test-fixtures"

    mkdir -p "${outdir}/internal/hcl"

    git reset --hard "${sha1_children}"
    for d in scanner strconv token; do
        cp -a "hcl/${d}" "${outdir}/internal/hcl/${d}"
    done

    # nuke irrelevant tests
    for tf in \
        basic_int_string.hcl \
        basic.json \
        decode_policy.hcl \
        decode_policy.json \
        decode_tf_variable.hcl \
        decode_tf_variable.json \
        flat.hcl \
        float.json \
        slice_expand.hcl \
        structure2.hcl \
        structure2.json \
        structure_flat.json \
        structure_flatmap.hcl \
        structure.hcl \
        structure.json \
        terraform_heroku.json \
        ; do
        rm -f "${outdir}/test-fixtures/${tf}"
    done
)

# fix imports
(
    cd "${outdir}"

    find internal -name '*.go' | xargs sed -i \
        's@github.com/hashicorp/hcl/hcl@github.com/hashicorp/consul/lib/oldhcl/internal/hcl@'
    find . -name '*.go' | xargs sed -i \
        's@github.com/hashicorp/hcl@github.com/hashicorp/consul/lib/oldhcl@'
)

# apply local patches
(
    cd "${basedir}"
    patch -d "${outdir}" -p0 < oldhcl-patch.diff
)

# Copy over all finalized files to their new location.

(
    cd "${basedir}"

    cp "${outdir}/LICENSE" "${finaldir}/LICENSE"

    for d in test-fixtures internal; do
        rm -rf "${finaldir}/${d}"
        cp -a "${outdir}/${d}" "${finaldir}/${d}"
    done
)

# Cleanup everything except the git clone to save on bandwidth.
rm -rf "${outdir}"
