# ParseRails HTTP server with OCR (Tesseract) and office support (LibreOffice).
# PDF parsing stays cgo-free (WASM); the runtime image only adds the optional
# external binaries the OCR and office features shell out to.
FROM golang:1.26 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/parse-server .

FROM debian:12-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        tesseract-ocr \
        libreoffice-core libreoffice-writer libreoffice-calc libreoffice-impress \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /bin/parse-server /bin/parse-server
EXPOSE 8080
ENTRYPOINT ["/bin/parse-server"]
