# Pure-WASM ParseRails: no cgo, no system libraries, fully static binary.
#
# Build from the REPO ROOT so the example's `replace => ../../` resolves to the
# local parserails source (no need to publish the module):
#
#   docker build -f examples/extract-text/Dockerfile -t parserails-extract .
FROM golang:1.26 AS build
WORKDIR /src
COPY . .
WORKDIR /src/examples/extract-text
# CGO disabled — the default WASM backend needs no C toolchain.
RUN CGO_ENABLED=0 go build -o /bin/extract-text .

FROM gcr.io/distroless/static-debian12
COPY --from=build /bin/extract-text /bin/extract-text
ENTRYPOINT ["/bin/extract-text"]
