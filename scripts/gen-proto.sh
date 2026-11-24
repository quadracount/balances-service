#!/usr/bin/env sh
# Regenera gen/balancespb. Requiere protoc, protoc-gen-go y protoc-gen-go-grpc.
# El paquete Go va con la opción M para no tocar el .proto compartido.

set -eu

PKG=github.com/quadracount/balances-service/gen/balancespb

mkdir -p gen/balancespb
protoc --proto_path=protos \
  --go_out=gen/balancespb --go_opt=paths=source_relative \
  --go_opt=Mbalances.proto=$PKG \
  --go-grpc_out=gen/balancespb --go-grpc_opt=paths=source_relative \
  --go-grpc_opt=Mbalances.proto=$PKG \
  balances.proto

echo "Stubs generados en gen/balancespb/"
