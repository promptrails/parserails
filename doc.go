// Package parserails provides fast, light, cgo-free document parsing for Go.
//
// It extracts text with spatial bounding boxes from PDFs using Google's PDFium
// engine compiled to WebAssembly (run via wazero), renders page screenshots, and
// supports pluggable OCR for scanned documents — without requiring CGO or any
// system-level native libraries.
//
// ParseRails is the Go counterpart to run-llama/liteparse.
package parserails
