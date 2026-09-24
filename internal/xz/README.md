# internal/xz

A copy of the decoder of `github.com/therootcompany/xz` v1.0.1, which is
`github.com/xi2/xz` with a `go.mod`, a Go translation of XZ Embedded. It
decodes the BCJ filters (x86, PowerPC, IA-64, ARM, ARM-Thumb, SPARC) and the
Delta filter, which `github.com/ulikunitz/xz` does not. oku reads an xz file
with ulikunitz first, and with this package when the file uses a filter chain.

The code is in the public domain under CC0, see LICENSE and AUTHORS. oku keeps
it as upstream wrote it, formatted with gofumpt and golines only. It has no
ARM64 or RISC-V BCJ filter.
