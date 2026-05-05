# eBPF Build Makefile
# Requires: clang, llvm, libbpf
#
# Usage:
#   make              - Build eBPF objects
#   make vmlinux.h    - Generate vmlinux.h from current kernel BTF
#   make clean        - Remove build artifacts
#   make deps         - Show dependency installation commands
#   make install      - Copy built objects to output directory

OUTPUT_DIR ?= ../internal/conntrack/bpf
CLANG ?= clang
LLVM_STRIP ?= llvm-strip
BPFTOOL ?= bpftool

# Architecture detection
ARCH := $(shell uname -m | sed 's/x86_64/x86/' | sed 's/aarch64/arm64/')

# Kernel headers - try multiple locations
KERN_INCLUDE ?= $(shell test -d /usr/include/bpf && echo /usr/include || echo /usr/include)

# C flags for eBPF compilation
CFLAGS = -g -O2 -Wall -Wno-unused-variable -Wno-unused-function
CFLAGS += -target bpf
CFLAGS += -D__TARGET_ARCH_$(ARCH)
CFLAGS += -I$(KERN_INCLUDE)
CFLAGS += -I.

# Tracepoint is always enabled for outgoing connections
# No compile-time flags needed

# Source and output files
SRCS = conntrack.bpf.c
OBJS = $(SRCS:.c=.o)
SKELS = $(SRCS:.c=.skel.h)

# vmlinux.h - generate from current kernel BTF (recommended for target compatibility)
.PHONY: all clean install deps help fallback vmlinux.h

vmlinux.h:
	@echo "Generating vmlinux.h from /sys/kernel/btf/vmlinux..."
	@if [ ! -f /sys/kernel/btf/vmlinux ]; then \
		echo "Error: /sys/kernel/btf/vmlinux not found. Is BTF enabled?"; \
		exit 1; \
	fi
	$(BPFTOOL) btf dump file /sys/kernel/btf/vmlinux format c > vmlinux.h
	@echo "vmlinux.h generated successfully"

all: $(OBJS) | $(OUTPUT_DIR)
	@echo "eBPF objects built successfully"
	@ls -la $(OBJS)

# Create output directory
$(OUTPUT_DIR):
	mkdir -p $(OUTPUT_DIR)

# Compile eBPF program to ELF object
# vmlinux.h is optional - use existing file if bpftool not available
%.o: %.c conntrack.bpf.h
	@echo "  CLANG   $@"
	$(CLANG) $(CFLAGS) -c $< -o $@
	@echo "  STRIP   $@"
	$(LLVM_STRIP) -g $@

# Generate BTF info (optional, for CO-RE)
%.btf.o: %.o
	@echo "  BTF     $@"
	$(LLVM_STRIP) -g $<

# Generate Go skeleton header (optional)
%.skel.h: %.o
	@echo "  SKEL    $@"
	$(BPFTOOL) gen skeleton $< > $@

# Install built objects to output directory
install: all
	@echo "Installing eBPF objects to $(OUTPUT_DIR)"
	cp -f $(OBJS) $(OUTPUT_DIR)/
	@echo "Installation complete"

clean:
	rm -f $(OBJS) $(SKELS) *.btf.o
	rm -rf $(OUTPUT_DIR)

# Show dependency installation commands
deps:
	@echo "=== eBPF Build Dependencies ==="
	@echo ""
	@echo "Ubuntu/Debian:"
	@echo "  sudo apt-get install -y clang llvm libbpf-dev linux-headers-$(shell uname -r)"
	@echo ""
	@echo "RHEL/CentOS/Fedora:"
	@echo "  sudo yum install -y clang llvm libbpf-devel kernel-devel-$(shell uname -r)"
	@echo "  # or for Fedora:"
	@echo "  sudo dnf install -y clang llvm libbpf-devel kernel-devel-$(shell uname -r)"
	@echo ""
	@echo "Arch Linux:"
	@echo "  sudo pacman -S clang llvm libbpf linux-headers"
	@echo ""
	@echo "bpftool (optional, for skeleton generation):"
	@echo "  Ubuntu/Debian: sudo apt-get install -y linux-tools-generic"
	@echo "  RHEL/CentOS: sudo yum install -y bpftool"

help:
	@echo "eBPF Build System"
	@echo ""
	@echo "Targets:"
	@echo "  all      - Build eBPF object files (default)"
	@echo "  clean    - Remove build artifacts"
	@echo "  install  - Copy built objects to output directory"
	@echo "  deps     - Show dependency installation commands"
	@echo "  help     - Show this help message"
	@echo ""
	@echo "Variables:"
	@echo "  CLANG        - Clang compiler (default: clang)"
	@echo "  LLVM_STRIP   - LLVM strip tool (default: llvm-strip)"
	@echo "  BPFTOOL      - bpftool for skeleton generation (default: bpftool)"
	@echo "  OUTPUT_DIR   - Output directory (default: ../internal/conntrack/bpf)"
	@echo "  KERN_INCLUDE - Kernel headers include path"
