#!/bin/bash

set -e

codecgen -d 102 -t ent -rt ent -o structs_ent.generated.go structs_ent.go
sed -i'' -e 's|"github.com/ugorji/go/codec|"github.com/hashicorp/go-msgpack/codec|g' structs_ent.generated.go
