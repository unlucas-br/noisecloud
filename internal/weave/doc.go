// Package weave implements a transport-agnostic erasure recovery codec.
//
// Weave is designed for projects that can detect whole-frame or whole-packet
// failures and want a compact alternative to per-frame Reed-Solomon parity.
// It splits a byte payload into systematic data frames and adds configurable
// rescue frames per block. Rescue frames are generated with a Cauchy matrix
// over GF(256), allowing recovery of missing data frames from any sufficient
// subset of surviving rescue frames.
//
// The package does not know about video, images, QR codes, networks, storage,
// or any other transport. Applications decide how to move the marshalled frames
// and can use ParseFrame to reject corrupted frames before reconstruction.
//
// For throughput, data frame payloads may reference the original input slice.
// Callers should not mutate input bytes until frames have been marshalled,
// transported, or copied.
//
// Long-running applications can use NewCodec to keep validated configuration,
// cached coefficient tables, scratch buffers, and recovered-payload workspace
// between calls. A Codec is meant for one worker at a time.
package weave
