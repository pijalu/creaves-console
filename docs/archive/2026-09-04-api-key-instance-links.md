# API key ↔ instance links

## Change

Instance administration now loads restricted webhook API keys and lists each key as a link to its detail page. API key index and detail pages link restricted keys back to their owning instance. Unrestricted keys remain explicitly unlinked as applicable because they have no owning instance.

## Validation

- `CGO_ENABLED=1 go test -tags sqlite -count=1 ./...`
- agent-browser authenticated admin click-through: instance page → restricted API key detail → owning instance; HTTP 200 responses observed.
