# eBPF Build Makefile
# Requires: clang, llvm, libbpf

OUTPUT ?= ../internal/conntrack
CLANG ?= clang
LLVM_STRIP ?= llvm-strip
BPFTOOL ?= bpftool

# Architecture
ARCH := $(shell uname -m | sed 's/x86_64/x86/' | sed 's/aarch64/arm64/')

# Kernel headers
KERN_INCLUDE ?= /usr/include

# C flags
CFLAGS = -g -O2 -Wall -target bpf
CFLAGS += -D__TARGET_ARCH_$(ARCH)
CFLAGS += -I$(KERN_INCLUDE)
CFLAGS += -I.

# Source files
SRCS = conntrack.bpf.c
OBJS = $(SRCS:.c=.o)

.PHONY: all clean

all: $(OBJS)

%.o: %.c
	$(CLANG) $(CFLAGS) -c $< -o $@
	@echo "  Compiled  $@"
	$(LLVM_STRIP) -g $@

# Generate BTF info (optional, for CO-RE)
%.btf.o: %.o
	$(LLVM_STRIP) -g $<
	@echo "  BTF       $@"

# Generate skeleton header (for Go binding)
%.skel.h: %.o
	$(BPFTOOL) gen skeleton $< > $@
	@echo "  Skeleton  $@"

clean:
	rm -f $(OBJS) *.skel.h *.btf.o

# Install dependencies helper
deps:
	@echo "Installing eBPF build dependencies..."
	@echo "Ubuntu/Debian: sudo apt-get install -y clang llvm libbpf-dev linux-headers-$(shell uname -r)"
	@echo "RHEL/CentOS: sudo yum install -y clang llvm libbpf-devel kernel-devel"
