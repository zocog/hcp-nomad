Metrics End to End Tests
========================

Note: not currently is use.

This folder contains scripts used for triggering a load test on AWS. This is intended to be used
before cutting a release to run the same  set of workloads against two different versions of Nomad.
It currently uses the end to end test terraform setup that relies on OSS build artifacts stored in
AWS S3.
