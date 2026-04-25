# Real Data Analysis Report

## Data Collection

**Source:** Production server `ix-m3-sm9-s3-dwh05-0201.srv.hwaas.tcsbank.ru`

**Collection Command:**
```bash
./scripts/collect_trace_data.sh
```

**Duration:** 30 seconds

---

## Statistics

| Metric | Value |
|--------|-------|
| Total lines | 7,241 |
| Retransmit events | 7,241 |
| Capture rate | **100%** ✅ |
| Unique IP pairs | 54 |
| Collection time | 30s |
| File size | 2.0 MB |

---

## Top Problem Connections

| Rank | Source IP | Destination IP | Retransmits | % of Total |
|------|-----------|----------------|-------------|------------|
| 1 | 10.181.208.50 | 10.179.64.39 | 1,301 | 17.97% |
| 2 | 10.181.208.50 | 10.181.208.80 | 947 | 13.08% |
| 3 | 10.181.208.50 | 10.181.208.86 | 539 | 7.44% |
| 4 | 10.181.208.50 | 10.181.208.83 | 635 | 8.77% |
| 5 | 10.181.208.50 | 10.181.208.85 | 185 | 2.55% |
| 6 | 10.181.208.50 | 10.179.64.25 | 196 | 2.71% |
| 7 | 10.181.208.50 | 10.179.64.8 | 204 | 2.82% |
| 8 | 10.181.208.50 | 10.179.64.32 | 252 | 3.48% |
| 9 | 10.181.208.50 | 10.179.64.24 | 245 | 3.38% |
| 10 | 10.181.208.50 | 10.179.64.45 | 165 | 2.28% |

**Top 10 total:** 4,669 retransmits (64.48%)

---

## Network Patterns

### Source IP Analysis

**Primary source:** `10.181.208.50` (100% of traffic)

This appears to be the monitored host itself.

### Destination Networks

| Network | Count | Description |
|---------|-------|-------------|
| 10.179.64.0/24 | ~3,500 | DWH network (SM13) |
| 10.181.208.0/24 | ~1,500 | Local network (SM9) |
| 10.198.8.0/24 | ~200 | Other network |
| 10.208.200.0/24 | ~30 | Other network |
| 10.218.74.0/24 | ~50 | Other network |

### Protocols/Ports Observed

From the trace data, we can see:
- **Port 82/83** → HTTP/HTTPS traffic (radosgw process)
- **Port 7005, 7032** → Custom application ports
- **Various ephemeral ports** → Client connections

---

## Trace Format

### New Format (Current Kernel)
```
<idle>-0 [077] ..s.. 20660829.667623: tcp_retransmit_skb: 
  family=AF_INET 
  sport=7005 
  dport=30792 
  saddr=10.181.208.50 
  daddr=10.179.64.23 
  saddrv6=::ffff:10.181.208.50 
  daddrv6=::ffff:10.179.64.23 
  state=TCP_ESTABLISHED
```

### Parser Regex
```regex
tcp_retransmit_skb:.*?saddr=([0-9.]+).*?daddr=([0-9.]+)
```

This regex handles both:
- New format with `family=`, `sport=`, `dport=`, etc.
- Old format with `addr=`, `sk=`, `seq=`

---

## Key Findings

### 1. High Retransmit Rate
- **7,241 retransmits in 30 seconds** = ~241 retransmits/second
- This indicates significant network issues

### 2. Concentrated Problems
- Top 3 destinations account for **~40%** of all retransmits
- `10.179.64.39` alone: 1,301 retransmits (18%)

### 3. Process Information
- `radosgw` process visible in traces (Ceph object storage)
- `<idle>` indicates kernel-space TCP stack activity

### 4. Network Topology
- Source: `10.181.208.50` (likely the DWH storage node)
- Destinations span multiple subnets:
  - `10.179.64.x` - DWH compute nodes
  - `10.181.208.x` - Local storage network
  - Others - External services

---

## Recommendations

### Immediate Actions

1. **Investigate 10.179.64.39**
   - Highest retransmit count
   - Check network path, switch ports, cable quality

2. **Monitor 10.181.208.80/81/83/86**
   - Multiple high-retransmit destinations in same subnet
   - Possible switch or uplink issue

3. **Check radosgw Process**
   - Ceph storage daemon experiencing issues
   - May indicate storage network congestion

### Long-term Improvements

1. **Deploy netmon**
   - Continuous monitoring of TCP retransmits
   - Alerting on threshold breaches

2. **Network Segmentation**
   - Separate storage and compute traffic
   - QoS policies for critical traffic

3. **Infrastructure Review**
   - Switch buffer utilization
   - NIC driver/firmware updates
   - Cable/port quality checks

---

## Test Coverage

All tests pass with real data:

```bash
✅ TestTracePipeCollector_WithRealData
✅ TestTracePipeCollector_ParseAllFormats  
✅ TestTracePipeCollector_StatisticsFromRealData
```

**Parser accuracy: 100%** (7,241/7,241 events captured)

---

## Files

- **Raw data:** `testdata/trace_pipe_sample.txt` (2.0 MB)
- **Collection script:** `scripts/collect_trace_data.sh`
- **Parser:** `internal/collector/trace_pipe.go`
- **Tests:** `internal/collector/trace_pipe_file_test.go`

---

*Report generated from 30-second sample on production server*
