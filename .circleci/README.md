# `.circleci/config.yml`

This directory contains the Nomad Enterprise CircleCI CI scripts. The CI
configuration adopts the OSS configuration and adds an Enterprise specific
workflow, `merge-oss`. The `merge-oss` workflow merges OSS main branch into the
Enterprise one every night.

To ease merging OSS CI changes, the Enterprise specific workflows/jobs live in
`./workflows` and `./jobs` directories and get merged with the OSS version using
the circleci local CLI.

### How to update merge-oss job

When updating `merge-oss` job, modify the job file, `./jobs/merge-oss.yml`, and
workflow file, ./workflows/merge-oss.yml, then config.yml

### How to generate config.yml

1. Ensure you have the latest [CircleCI local CLI](https://circleci.com/docs/2.0/local-cli/\#installation) installed.
2. Pull the latest OSS script file: `git fetch oss main && git checkout oss/main .circleci/config.yml`
   * Run `git remote add oss https://github.com/hashicorp/nomad.git`
3. Run run `make -C .circleci config.yml`.

### How to handle a failing merge-oss

If the nightly `merge-oss` job fails due to a merge conflict:

1. Update local branch to latest main
2. Run `scripts/enterprise/merge-oss.sh`
3. Inspect git status and resolve merge conflicts
4. Push to nomad-enterprise repo and open a PR
