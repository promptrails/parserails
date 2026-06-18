//go:build !parserails_cgo

package parserails

import (
	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// Backend identifies the active PDFium backend. This is the default, cgo-free
// WebAssembly build; build with -tags parserails_cgo for the native backend.
const Backend = "wasm"

type poolConfig struct{ minIdle, maxIdle, maxTotal int }

// newPool starts a pure-Go PDFium pool running under the wazero WebAssembly
// runtime. No cgo and no system libraries are required.
func newPool(c poolConfig) (pdfium.Pool, error) {
	return webassembly.Init(webassembly.Config{
		MinIdle:  c.minIdle,
		MaxIdle:  c.maxIdle,
		MaxTotal: c.maxTotal,
	})
}
