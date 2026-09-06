# Release Process

Releases are created by `.github/workflows/release.yml` after a `v*` tag is
pushed. Supported release artifacts are Linux `amd64` only. CI may
cross-compile Go code for other platforms, but those binaries are not runtime
qualified and are not published.

## Qualification

Before tagging a conntrack release candidate:

1. Build `conntrack-linux-amd64` from the intended commit on Linux with fresh
   `bpf/*.o` copied to `pkg/embedded/bpf/`.
2. Run the same binary across the supported kernel matrix:

   ```powershell
   .\tests\conntrack\e2e\run-matrix.ps1 `
     -BinaryPath .\dist\conntrack-linux-amd64
   ```

3. From the bundle, verify `install → start → /ready → /metrics → restart →
   deinstall` on a qualification host:

   ```bash
   sudo tests/conntrack/e2e/qualify-bundle.sh \
     ./conntrack-<version>-linux-amd64.tar.gz
   ```
4. On an active host running the previous release, qualify upgrade and rollback:

   ```bash
   sudo tests/conntrack/e2e/qualify-upgrade.sh ./conntrack-linux-amd64
   ```
5. Confirm CI, Security Scan, eBPF Build & Verify, and Docker Publish are green.

## Creating a release

The project follows semantic versioning. Use `vMAJOR.MINOR.PATCH` or a suffix
such as `-rc.1`.

```bash
git switch main
git pull --ff-only origin main
git tag -a v2.2.0 -m "Release v2.2.0"
git push origin v2.2.0
```

The workflow rebuilds eBPF objects, embeds them, builds netmon and conntrack,
creates runnable bundles and raw binaries, generates `checksums.txt`, and
creates a draft GitHub Release using `docs/releases/<version>.md`.
Validate the exact downloaded draft assets and checksums, then publish the draft
with `gh release edit <version> --draft=false --latest`.

Expected assets:

- `netmon-linux-amd64` and `netmon-<version>-linux-amd64.tar.gz`;
- `conntrack-linux-amd64` and `conntrack-<version>-linux-amd64.tar.gz`;
- `irqdiag-linux-amd64` for read-only IRQ/NUMA support reports;
- `checksums.txt`.

Verify downloaded assets with:

```bash
sha256sum -c checksums.txt --ignore-missing
```

Do not create or upload ARM artifacts until an ARM Linux runtime host and an
architecture-correct eBPF build/qualification path are available.
