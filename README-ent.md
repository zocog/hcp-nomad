# Nomad Enterprise

## Building

In order to download dependencies from private GitHub repositories set:

```
GOPRIVATE=github.com/hashicorp
```

And in your `.gitconfig` file add:

```
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
```

### Vagrant

We have several dependencies (Sentinel, Consul-Enterprise, Licensing)
that are in private GitHub repositories. To build these in Vagrant,
you'll need to provide Vagrant with a ssh key and configuration as a
one-time setup task.

On the Vagrant box, create a key, authenticate the host key, and
create a git config file that causes the go toolchain to default to
ssh access:

```
ssh-keygen -t ed25519 -C "yourname+vagrant@hashicorp.com"
ssh -T git@github.com

cat <<EOF > ~/.gitconfig
[url "git@github.com:"]
	insteadOf = https://github.com/
EOF
```

Then upload the public key found at `/home/vagrant/id_ed25519.pub` to
your GitHub profile at https://github.com/settings/keys and authorize
it with SSO.

## Licensing

As of Nomad 1.1.0, Nomad Enterprise requires a license to start.

You can grab a license under the `nomad-testing` org at
[https://license.hashicorp.services](https://license.hashicorp.services/customers/dae084a1-971f-bfe2-31a8-36f6c1936527)

If neccessary, create a new license with the module(s) you want features
for. Ensure that this license is treated like a secret, do not commit it to
any public repos as there is no way to track it and it will be valid until the
license expires.

**NOTE**: A learn [tutorial](https://learn.hashicorp.com/tutorials/nomad/hashicorp-enterprise-license?in=nomad/enterprise)
is available for instructions on how to install the enterprise license.

### Generating a license

- go to https://license.hashicorp.services/ log in
- search for nomad-testing org
  (https://license.hashicorp.services/customers/dae084a1-971f-bfe2-31a8-36f6c1936527)
- see if there's an unexpired license that will meet your needs, use that or
- create a new license that expires < 1 year

To activate all Enterprise features, use v2 (for Nomad 1.1.0+) with the following Flags value:

```json
{
  "modules": [
    "governance-policy",
    "multicluster-and-efficiency"
  ]
}
```

### Running and Testing Nomad Enterprise

You can load the content of the license via the `NOMAD_LICENSE` environment
variable (ex. `export NOMAD_LICENSE=$(cat license.hclic)`), or provide the
file path to the license file in either the
[`server.license_path`](https://www.nomadproject.io/docs/configuration/server#license_path)
configuration field or in the `NOMAD_LICENSE_PATH` environment variable.

The `api` package tests against a real Nomad binary, so you'll need to make
sure that `NOMAD_LICENSE` is set when you're running the tests in the `api`
package.

Also, remember that when testing with the `-dev` server, `sudo` will wipe out
your shell session's environment variables. So you'll need to either switch to
a root user before setting the `NOMAD_LICENSE` or `NOMAD_LICENSE_PATH`
environment variable, or use a configuration file with the
`server.license_path` set even in `-dev` mode.

### Alternative to License Files

As an alternative, you can use the `on_prem_modules` tag for your build, which
will generate the binary that we ship to ENT customers who have opted out of
our current licensing.

## Housekeeping Items

### Updating Enterprise with OSS Code

The Enterprise codebase should be in sync with the [OSS
repository](https://github.com/hashicorp/nomad), by merging the OSS codebase
periodically. The branches that are kept in sync automatically are `main` and
the last three `release/<MAJOR>.<MINOR>.x` release branches.

This process is automated via the
[`merge-oss-cron.yaml`](https://github.com/hashicorp/nomad-enterprise/blob/main/.github/workflows/merge-oss-cron.yaml)
GitHub Action workflow. This workflow can be manually triggered from the UI by
visiting the
[`Merge OSS to ENT
Nightly`](https://github.com/hashicorp/nomad-enterprise/actions/workflows/merge-oss-cron.yaml)
Actions page and clicking the `Run workflow` button. Select the `main` branch
to run the latest merge logic, or pick your own branch if you are testing
changes to the workflow itself.

You can also merge two arbitrary branches using the
[`merge-oss-manualy.yaml`](https://github.com/hashicorp/nomad-enterprise/blob/main/.github/workflows/merge-oss-manualy.yaml)
workflow.
To trigger this workflow, visit the [`Merge OSS to ENT
Manually`](https://github.com/hashicorp/nomad-enterprise/actions/workflows/merge-oss-manualy.yaml)
Actions page, click `Run workflow`, and select the source (OSS) branch and the
destination (ENT) branch.

Manual merging is necessary sometimes, due to merge conflicts. When this
happens, the workflow fails and it outputs a series of commands that can be
used to run the merge locally and allow you to manually fix the conflicts.
