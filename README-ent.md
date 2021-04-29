# Nomad Enterprise

As of Nomad 1.1.0, Nomad Enterprise requires a license to start.

You can grab a license under the `nomad-testing` org at
[https://license.hashicorp.services](https://license.hashicorp.services/customers/dae084a1-971f-bfe2-31a8-36f6c1936527)

If neccessary, create a new license with the module(s) you want features
for. Ensure that this license is treated like a secret, do not commit it to
any public repos as there is no way to track it and it will be valid until the
license expires.

## Generating a license

- go to https://license.hashicorp.services/ log in
- search for nomad-testing org
  (https://license.hashicorp.services/customers/dae084a1-971f-bfe2-31a8-36f6c1936527)
- see if there's an unexpired license that will meet your needs, use that or
- create a new license that expires < 1 year

## Running and Testing Nomad Enterprise

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

## Alternative to License Files

As an alternative, you can use the `on_prem_modules` tag for your build, which
will generate the binary that we ship to ENT customers who have opted out of
our current licensing.
