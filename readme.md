# Bandwidth Limiter Plugin for Traefik

 bandwidth limiting middleware plugin for Traefik that provides fine-grained control over data transfer rates. This plugin supports per-backend and per-client IP rate limiting with automatic memory management and persistent state storage.

## Features

### Core Bandwidth Limiting
- **Token Bucket Algorithm**: Implements efficient rate limiting using the token bucket pattern
- **Per-Backend Limits**: Set different bandwidth limits for different backend services
- **Per-Client IP Limits**: Configure specific rates for individual client IP addresses
- **Configurable Burst Sizes**: Allow temporary bursts above the average rate limit
- **IPv4 and IPv6 Support**: Works with both IP address formats

### Memory Management and Persistence
- **Automatic Bucket Cleanup**: Periodically removes unused rate limiters to prevent memory leaks
- **File-Based Persistence**: Saves rate limiting state to disk for persistence across restarts
- **Configurable Cleanup Intervals**: Tune memory usage for your specific traffic patterns
- **Graceful Shutdown**: Ensures all state is saved before the plugin stops

### Production-Ready Features
- **Thread-Safe Operations**: Concurrent request handling without race conditions
- **Minimal Performance Impact**: Optimized for high-throughput environments
- **Detailed Logging**: Monitor cleanup operations and persistence events
- **Client IP Detection**: Smart extraction of real client IPs behind proxies

## Quick Start

### Installation

1. Install the plugin in your Traefik instance:

```bash
# Add to your Traefik static configuration
experimental:
  plugins:
    bandwidthlimiter:
      moduleName: github.com/hhftechnology/bandwidthlimiter
      version: v1.0.0
```

2. Create a basic middleware configuration:

```yaml
# dynamic.yml
http:
  middlewares:
    my-bandwidth-limiter:
      plugin:
        bandwidthlimiter:
          defaultLimit: 1048576  # 1 MB/s
          burstSize: 5242880     # 5 MB burst
```

3. Apply the middleware to your routes:

```yaml
http:
  routers:
    my-service:
      rule: "Host(`example.com`)"
      service: my-service
      middlewares:
        - my-bandwidth-limiter
```

## Configuration Reference

### Basic Configuration

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `defaultLimit` | int64 | 1048576 | Default bandwidth limit in bytes per second |
| `burstSize` | int64 | 10x defaultLimit | Maximum burst size in bytes |
| `backendLimits` | map[string]int64 | {} | Backend-specific limits |
| `clientLimits` | map[string]int64 | {} | Client IP-specific limits |

### Advanced Configuration

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `bucketMaxAge` | int64 | 3600 | Maximum age of unused buckets before cleanup (seconds) |
| `cleanupInterval` | int64 | 300 | Interval between cleanup runs (seconds) |
| `persistenceFile` | string | "" | File path for persistent storage (disabled if empty) |
| `saveInterval` | int64 | 60 | Interval between saves to persistence file (seconds) |

## Configuration Examples

### Basic Rate Limiting

```yaml
http:
  middlewares:
    simple-limiter:
      plugin:
        bandwidthlimiter:
          defaultLimit: 524288      # 512 KB/s
          burstSize: 2621440        # 2.5 MB burst
```

### Per-Backend Limits

```yaml
http:
  middlewares:
    backend-limiter:
      plugin:
        bandwidthlimiter:
          defaultLimit: 1048576     # 1 MB/s default
          backendLimits:
            api.example.com: 2097152      # 2 MB/s for API
            static.example.com: 524288    # 512 KB/s for static content
            video.example.com: 10485760   # 10 MB/s for video streaming
```

### Client-Specific Limits

```yaml
http:
  middlewares:
    client-limiter:
      plugin:
        bandwidthlimiter:
          defaultLimit: 524288      # 512 KB/s for regular users
          clientLimits:
            203.0.113.100: 5242880       # 5 MB/s for premium client
            203.0.113.101: 2097152       # 2 MB/s for business client
            "2001:db8::1": 10485760      # 10 MB/s for IPv6 client
```

### Production Configuration with Persistence

```yaml
http:
  middlewares:
    production-limiter:
      plugin:
        bandwidthlimiter:
          # Basic limits
          defaultLimit: 1048576
          burstSize: 5242880
          
          # Memory management
          bucketMaxAge: 1800       # 30 minutes
          cleanupInterval: 300     # 5 minutes
          
          # Persistence
          persistenceFile: "/plugins-storage/bandwidth-state.json"
          saveInterval: 60         # 1 minute
          
          # Advanced limits
          backendLimits:
            api.example.com: 2097152
            critical.example.com: 10485760
          clientLimits:
            192.168.1.100: 10485760
            "fd00::1": 5242880
```

## Bandwidth Value Reference

Quickly convert between human-readable speeds and configuration values:

| Speed | Configuration Value |
|-------|-------------------|
| 128 Kbps | 16384 |
| 256 Kbps | 32768 |
| 512 Kbps | 65536 |
| 1 Mbps | 131072 |
| 2 Mbps | 262144 |
| 5 Mbps | 655360 |
| 10 Mbps | 1310720 |
| 25 Mbps | 3276800 |
| 50 Mbps | 6553600 |
| 100 Mbps | 13107200 |

**Formula**: Mbps × 131,072 = bytes/second

## Best Practices

### Memory Management

Configure cleanup based on your traffic patterns:

**High-Traffic Sites:**
```yaml
bandwidthlimiter:
  bucketMaxAge: 600        # 10 minutes
  cleanupInterval: 60      # 1 minute
```

**Medium-Traffic Sites:**
```yaml
bandwidthlimiter:
  bucketMaxAge: 1800       # 30 minutes
  cleanupInterval: 300     # 5 minutes
```

**Low-Traffic Sites:**
```yaml
bandwidthlimiter:
  bucketMaxAge: 3600       # 1 hour
  cleanupInterval: 600     # 10 minutes
```

### Persistence Configuration

**Critical Applications:**
```yaml
bandwidthlimiter:
            persistenceFile: "/plugins-storage/bandwidth-critical.json"
  saveInterval: 30         # Save every 30 seconds
```

**Standard Applications:**
```yaml
bandwidthlimiter:
            persistenceFile: "/plugins-storage/bandwidth-standard.json"
  saveInterval: 60         # Save every minute
```

**Development/Testing:**
```yaml
bandwidthlimiter:
  persistenceFile: "/tmp/bandwidth-dev.json"
  saveInterval: 300        # Save every 5 minutes
```

## Deployment Scenarios

### Docker Compose

```yaml
version: '3.8'
services:
  traefik:
    image: traefik:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./traefik.yml:/etc/traefik/traefik.yml:ro
      - ./dynamic.yml:/etc/traefik/dynamic.yml:ro
      - ./traefik/plugins-storage:/plugins-storage:rw
      - ./traefik/plugins-storage:/plugins-local:rw
    ports:
      - "80:80"
      - "443:443"


```

### Kubernetes

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: traefik-bandwidth-config
data:
  dynamic.yml: |
    http:
      middlewares:
        bandwidth-limiter:
          plugin:
            bandwidthlimiter:
              persistenceFile: "/plugins-storage/bandwidth-state.json"
              defaultLimit: 1048576

---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: traefik-bandwidth-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 100Mi
```

### Bare Metal

```bash
# Create directories
sudo mkdir -p /plugins-storage
sudo chown traefik:traefik /plugins-storage
sudo chmod 755 /plugins-storage

# Traefik configuration
cat > /etc/traefik/dynamic.yml << EOF
http:
  middlewares:
    bandwidth-limiter:
      plugin:
        bandwidthlimiter:
          persistenceFile: "/plugins-storage/bandwidth-state.json"
          defaultLimit: 1048576
EOF
```

## Monitoring and Observability

### Log Monitoring

Watch for these log messages:

```
# Successful operations
INFO: Cleanup removed 150 unused buckets (kept 500 active buckets)
INFO: Saved 500 buckets to /plugins-storage/bandwidth-state.json
INFO: Loaded 450 buckets from /plugins-storage/bandwidth-state.json

# Potential issues
WARNING: Failed to save buckets: disk full
ERROR: Failed to load persisted buckets: file corrupt
```

### Metrics to Track

1. **Bucket Count**: Monitor active buckets over time
2. **Cleanup Efficiency**: Track removed vs. retained buckets
3. **File Size**: Watch persistence file growth
4. **Save/Load Times**: Ensure operations complete quickly

### Example Monitoring Script

```bash
#!/bin/bash
# bandwidth-monitor.sh

# Check memory usage
echo "Memory usage:"
ps aux | grep traefik | grep -v grep | awk '{print $6/1024 " MB"}'

# Check file size
echo "Persistence file size:"
du -h /plugins-storage/bandwidth-state.json

# Count active connections
echo "Active bandwidth buckets:"
grep -o '"key":' /plugins-storage/bandwidth-state.json | wc -l

# Recent cleanup activity
echo "Recent cleanup events:"
journalctl -u traefik --since "1 hour ago" | grep "Cleanup removed"
```

## Troubleshooting

### Common Issues

**High Memory Usage**
- Reduce `bucketMaxAge`
- Increase cleanup frequency (`cleanupInterval`)
- Check for memory leaks in logs

**Slow Response Times**
- Increase `burstSize` for initial data transfer
- Optimize `defaultLimit` values
- Consider hardware resources

**Missing Rate Limits After Restart**
- Verify `persistenceFile` path is correct
- Check file permissions
- Ensure directory exists and is writable

**File Permission Errors**
```bash
# Fix ownership and permissions
sudo chown traefik:traefik /plugins-storage/bandwidth-state.json
sudo chmod 644 /plugins-storage/bandwidth-state.json
```

### Debug Mode

Enable detailed logging in Traefik:

```yaml
# traefik.yml
log:
  level: DEBUG
  
# Or via command line
--log.level=DEBUG
```

## Advanced Usage

### Load Balancer Environments

For multiple Traefik instances:

```yaml
# Instance 1
bandwidthlimiter:
  persistenceFile: "/shared/storage/bandwidth-node1.json"

# Instance 2
bandwidthlimiter:
  persistenceFile: "/shared/storage/bandwidth-node2.json"

# Use external synchronization for shared state
```

### Backup and Disaster Recovery

```bash
#!/bin/bash
# bandwidth-backup.sh

# Create timestamped backup
cp /plugins-storage/bandwidth-state.json \
   /backup/bandwidth-$(date +%Y%m%d-%H%M%S).json

# Keep only last 30 days of backups
find /backup -name "bandwidth-*.json" -mtime +30 -delete

# Verify backup integrity
if ! jq . /backup/bandwidth-latest.json >/dev/null 2>&1; then
  echo "ERROR: Backup file is corrupted"
  exit 1
fi
```

### Rate Limit Development

Test configurations locally:

```yaml
# development.yml
http:
  middlewares:
    dev-bandwidth:
      plugin:
        bandwidthlimiter:
          # Fast cleanup for testing
          bucketMaxAge: 300
          cleanupInterval: 60
          
          # Local persistence
          persistenceFile: "/tmp/bandwidth-dev.json"
          
          # Test limits
          defaultLimit: 65536  # 64 KB/s
          clientLimits:
            127.0.0.1: 1048576  # 1 MB/s for localhost
```

## Performance Tuning

### Optimization Guidelines

1. **Bucket Management**
   - Start with conservative `bucketMaxAge` (1-2 hours)
   - Gradually reduce based on memory usage
   - Monitor cleanup efficiency

2. **Persistence Settings**
   - Balance between data safety and performance
   - Use faster storage for persistence files
   - Consider disabling in development

3. **Resource Planning**
   - Estimate: 200 bytes per active bucket
   - Plan for peak traffic scenarios
   - Monitor during traffic spikes

### Performance Benchmarks

Typical performance characteristics:

| Metric | Value |
|--------|-------|
| Request overhead | ~1-2ms |
| Cleanup duration | ~50-100ms per 1000 buckets |
| Save operation | ~100-200ms per 1000 buckets |
| Memory per bucket | ~200 bytes |
| File size per bucket | ~200 bytes (JSON) |

## Support and Contributing

### Reporting Issues

When reporting issues, include:

1. Traefik version
2. Plugin configuration
3. Traffic patterns
4. Error logs
5. Memory usage statistics

### Feature Requests

We welcome contributions for:

- Additional rate limiting algorithms
- Enhanced metrics and monitoring
- Integration with external storage systems
- Advanced synchronization between instances

### License

This plugin is licensed under the Apache 2.0 License. See LICENSE file for details.

## Appendix

### Command Line Tools

```bash
# Query bucket states
jq '.[] | select(.key | contains("192.168.1.100"))' /plugins-storage/bandwidth-state.json

# Count buckets by client IP
jq -r '.[].key' /plugins-storage/bandwidth-state.json | cut -d: -f1 | sort | uniq -c

# Find high-usage clients
jq '.[] | {key, tokens, limit} | select(.tokens < .limit * 0.1)' /plugins-storage/bandwidth-state.json
```

### Integration Examples

**Prometheus Metrics (Custom Endpoint)**
```go
// Add to custom metrics endpoint
type BandwidthMetrics struct {
    ActiveBuckets    int64
    TotalRequests    int64
    BytesTransferred int64
}
```

**Grafana Dashboard Query**
```promql
# Active bandwidth buckets
traefik_bandwidth_active_buckets

# Bandwidth utilization
rate(traefik_bandwidth_bytes_transferred[5m])
```

This comprehensive plugin provides enterprise-grade bandwidth limiting capabilities for Traefik, combining ease of use with advanced features for production environments. Start with the basic configuration and gradually enable advanced features as your needs grow.