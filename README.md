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
- The artifact itself is built by `nitrovin-import` in the nitrovin repo and
  will be embedded here, one data month per release. Versioning and the first
  tag are decided by nitrovin's release policy.

The data is NHTSA's, a work of the United States Government; see `NOTICE`.
The code is MIT.
