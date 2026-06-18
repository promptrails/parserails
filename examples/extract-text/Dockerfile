# Pure-WASM ParseRails: no cgo, no system libraries, fully static binary.
FROM golang:1.26 AS build
WORKDIR /src
COPY . .
# CGO disabled — the default WASM backend needs no C toolchain.
RUN CGO_ENABLED=0 go build -o /bin/extract-text .

FROM gcr.io/distroless/static-debian12
COPY --from=build /bin/extract-text /bin/extract-text
ENTRYPOINT ["/bin/extract-text"]
