# Native PDFium build (-tags parserails_cgo). This image installs libpdfium and
# builds with CGO enabled for maximum throughput. Compare with the cgo-free WASM
# examples, which need none of this.
FROM golang:1.26-bookworm AS build

# --- Install libpdfium (prebuilt) -------------------------------------------
# Prebuilt binaries: https://github.com/bblanchon/pdfium-binaries
ARG PDFIUM_VERSION=6996
ARG PDFIUM_ARCH=linux-x64
RUN apt-get update && apt-get install -y --no-install-recommends \
        pkg-config ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /opt/pdfium \
    && curl -fsSL "https://github.com/bblanchon/pdfium-binaries/releases/download/chromium%2F${PDFIUM_VERSION}/pdfium-${PDFIUM_ARCH}.tgz" \
        | tar -xz -C /opt/pdfium \
    && install -m644 /opt/pdfium/lib/libpdfium.so /usr/lib/libpdfium.so \
    && mkdir -p /usr/include/pdfium \
    && cp -r /opt/pdfium/include/* /usr/include/pdfium/ \
    && printf 'prefix=/usr\nlibdir=${prefix}/lib\nincludedir=${prefix}/include/pdfium\n\nName: pdfium\nDescription: PDFium\nVersion: %s\nLibs: -L${libdir} -lpdfium\nCflags: -I${includedir}\n' "${PDFIUM_VERSION}" \
        > /usr/lib/pkgconfig/pdfium.pc

# --- Build ------------------------------------------------------------------
WORKDIR /src
COPY . .
RUN CGO_ENABLED=1 go build -tags parserails_cgo -o /bin/native-cgo .

# --- Runtime ----------------------------------------------------------------
FROM debian:12-slim
COPY --from=build /usr/lib/libpdfium.so /usr/lib/libpdfium.so
COPY --from=build /bin/native-cgo /bin/native-cgo
ENTRYPOINT ["/bin/native-cgo"]
