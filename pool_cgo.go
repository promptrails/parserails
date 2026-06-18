//go:build parserails_cgo

package parserails

import (
	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/single_threaded"
)

// Backend identifies the active PDFium backend. This is the native cgo build
// (-tags parserails_cgo): it links libpdfium directly for much higher
// throughput and far lower memory than the WebAssembly default, at the cost of
// requiring libpdfium and a C toolchain at build/run time.
const Backend = "cgo"

type poolConfig struct{ minIdle, maxIdle, maxTotal int }

// newPool starts a native, in-process PDFium pool. PDFium calls are serialized
// by a shared mutex (single-threaded mode), which needs no external worker
// binary; pool-size hints from poolConfig do not apply here.
func newPool(_ poolConfig) (pdfium.Pool, error) {
	return single_threaded.Init(single_threaded.Config{}), nil
}
