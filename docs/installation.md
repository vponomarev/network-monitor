# Installation Guide

Production releases contain only Linux `amd64` binaries and bundles for
`netmon` and `conntrack`, plus the read-only `irqdiag` support tool. Use a host with BTF at
`/sys/kernel/btf/vmlinux`; the maintained kernel matrix is 5.15, 6.1, 6.8, and
6.12.

## Conntrack

The raw conntrack binary is self-installing:

```bash
wget https://github.com/vponomarev/network-monitor/releases/latest/download/conntrack-linux-amd64
chmod +x conntrack-linux-amd64
sudo ./conntrack-linux-amd64 install
sudo systemctl enable --now conntrack
curl --fail http://127.0.0.1:9876/ready
```

The command installs `/usr/local/bin/conntrack`, creates
`/etc/conntrack/config.yaml` only when it does not already exist, installs the
systemd unit, and reloads systemd. Remove managed files while preserving config:

```bash
sudo /usr/local/bin/conntrack deinstall
```

## Netmon bundle

Replace `<version>` with a release tag such as `v2.3.0`:

```bash
wget https://github.com/vponomarev/network-monitor/releases/download/<version>/netmon-<version>-linux-amd64.tar.gz
tar -xzf netmon-<version>-linux-amd64.tar.gz
cd netmon-<version>-linux-amd64
sudo install -m 0755 netmon /usr/local/bin/netmon
sudo mkdir -p /etc/netmon
sudo cp configs/*.yaml /etc/netmon/
sudo cp netmon.service /etc/systemd/system/netmon.service
sudo systemctl daemon-reload
sudo systemctl enable --now netmon
curl --fail http://127.0.0.1:9876/ready
```

Review the example configuration before production use, especially bind
address, bearer token, metadata paths, and metric cardinality.

## Verify artifacts

```bash
wget https://github.com/vponomarev/network-monitor/releases/download/<version>/checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

## Collect IRQ/NUMA diagnostics

```bash
wget https://github.com/vponomarev/network-monitor/releases/latest/download/irqdiag-linux-amd64
chmod +x irqdiag-linux-amd64
./irqdiag-linux-amd64 --output irq-report.json
```

The report is JSON and may be collected without root on systems where sysfs and
procfs are world-readable. It contains host identifiers and network addresses;
review it before sharing outside the intended support channel.

## Build from source

Go 1.25+, Clang/LLVM, libbpf headers, make, and Git are required:

```bash
git clone https://github.com/vponomarev/network-monitor.git
cd network-monitor
go mod download
make ebpf-build
make build
```

`make build-conntrack` rebuilds the eBPF object before compiling the binary.
Runtime qualification must still be performed on Linux as root; a successful
cross-compilation is not sufficient.

## Troubleshooting

```bash
uname -r
test -r /sys/kernel/btf/vmlinux
sudo journalctl -u netmon -n 100 --no-pager
sudo journalctl -u conntrack -n 100 --no-pager
sudo bpftool prog show
```

Do not install `pktloss` for production. ARM artifacts are intentionally not
published.
