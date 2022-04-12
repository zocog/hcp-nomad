#!/usr/bin/env bash
set -e

# ensure that ent and pro structs don't get auto generated without tags
FILES="$(ls ./*.go | grep -v -e _test.go -e .generated.go -e _ent.go -e _pro.go -e _ent_ -e _pro_ | tr '\n' ' ')"
codecgen \
    -c github.com/hashicorp/go-msgpack/codec \
    -st codec \
    -d 100 \
    -t codegen_generated \
    -o structs.generated.go \
    ${FILES}
