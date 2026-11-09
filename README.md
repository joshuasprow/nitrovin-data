# nitrovin-data

The data side of [nitrovin](https://github.com/joshuasprow/nitrovin), an
offline VIN decoder with parity to NHTSA's vPIC.

- `pkg/artifact` reads and writes the artifact: one month of NHTSA's vPIC
  dump, restricted to the tables `spVinDecode` reads, as fixed-width column
  arrays sorted by their lookup key with one interned string arena. It is read
  in place from embedded bytes or an mmap'd file. The layout is documented in
  the package (`go doc github.com/joshuasprow/nitrovin-data/pkg/artifact`); why
  this format is nitrovin's
  [ADR 0003](https://github.com/joshuasprow/nitrovin/blob/main/docs/adr/0003-custom-indexed-artifact-embedded-raw.md).
- `pkg/embedded` is one data month's artifact, `go:embed`-ed raw:
  `embedded.Artifact()`, `embedded.Month`, `embedded.BLAKE3`. Its
  `manifest.json` records the artifact (format, month, size, blake3, rows per
  table), NHTSA's dump (URL, size, SHA-256), the oracle's collation and the
  nitrovin commit whose `cmd/nitrovin-import` built it. Importing it adds about
  61 MB to a binary; `pkg/artifact` alone adds nothing.

## Versions and releases

Tags are `v<format>.<YYYYMM>.<rebuild>`: `v1.202610.0` is artifact format 1,
data month 2026_10, first build of that month
([ADR 0007](https://github.com/joshuasprow/nitrovin/blob/main/docs/adr/0007-release-and-versioning.md)).
nitrovin pins one in its `go.mod`. Every release keeps, forever, the
artifact, NHTSA's dump zip (so the oracle can re-run a month NHTSA no longer
hosts), `manifest.json`, `checksums.txt`, and a cosign keyless bundle for each
of those, signed by `.github/workflows/release.yml` on `main`:

```sh
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity https://github.com/joshuasprow/nitrovin-data/.github/workflows/release.yml@refs/heads/main \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
sha256sum -c checksums.txt
```

`release.yml` rebuilds the embedded month from the dump on every pull
request and requires the committed artifact and manifest byte for byte. On
`main`, dispatched or on a push that changes `pkg/embedded/manifest.json`, it
does the same, then tags the commit and publishes the release.
`scripts/build.sh` is that build, runnable on a workstation with a nitrovin
checkout:

```sh
../nitrovin/scripts/oracle.sh up
scripts/build.sh ../nitrovin 2026_10 /tmp/out
cmp /tmp/out/nitrovin-2026_10.artifact pkg/embedded/nitrovin.artifact
```

A new month replaces `pkg/embedded/nitrovin.artifact` and `manifest.json`
with `build.sh`'s output and updates `Month` and `BLAKE3`. nitrovin's monthly
refresh (its `.github/workflows/refresh.yml`) does that unattended: once the
month passes nitrovin's parity gates it pushes the commit to `main` with a
deploy key, naming its run in a `Refresh-Run:` trailer, and the release also
carries that run's `stale-cache.jsonl` (the calls on which stock and freshened
Postgres differ) and `parity-<month>.tar.gz` (the frozen answer key).

The data is NHTSA's, a work of the United States Government; see `NOTICE`.
The code is MIT.
