# Dependency security fixes

This change addresses GHSA-2v4p-qf9q-27wj and GHSA-q7pp-wcgr-pffx.

- Upgrade gRPC-Go from 1.83.1 to 1.83.2, which fixes the missing-authority xDS server panic. The module upgrade also advances its required x/net, x/text and x/crypto dependencies.
- Remove disintegration/imaging from both production and test dependencies. Decode images using Go and x/image format decoders, resize using x/image/draw Catmull-Rom, and encode with image/jpeg. JPEG, PNG, GIF, BMP, TIFF and WebP decoding remains registered. Aspect ratio, no-upscale behavior, payload limits and the small-image passthrough path are preserved. Resampling pixels can differ from the previous Lanczos implementation.
- Keep reduced dimensions within the configured maximum during payload retries, including maximum dimensions below 256 pixels.

Validation on Linux:

```sh
go test ./internal/vision ./tests/internal/vision
go build -mod=readonly -o cyberstrike-ai ./cmd/server
go mod verify
```

Regression tests cover normal image resizing, passthrough, file-size rejection, paletted TIFF conversion and malformed/truncated TIFF errors. Malformed TIFF cases are defensive regressions, not a reproduction of the advisory's original proof of concept.

Rebuild and replace the executable to use the fixed dependencies; editing go.mod does not update an already running process. No database migration or configuration change is required. Rolling back the binary restores its previous dependency vulnerabilities.

References: [gRPC advisory](https://github.com/grpc/grpc-go/security/advisories/GHSA-2v4p-qf9q-27wj), [imaging advisory](https://github.com/advisories/GHSA-q7pp-wcgr-pffx).
