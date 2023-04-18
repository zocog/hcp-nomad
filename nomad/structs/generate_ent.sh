#!/bin/bash

set -e

codecgen \
    -c github.com/hashicorp/go-msgpack/v2/codec \
    -st codec \
    -d 102 \
    -t ent \
    -rt ent \
    -o structs_ent.generated.go \
    structs_ent.go
