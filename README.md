# rhobs-synthetics-agent
RHOBS Synthetic Monitoring Agent

## Testing

Run unit tests:
```bash
go test ./cmd/agent -v
```

Run tests with coverage:
```bash
go test ./cmd/agent -cover
```

Run benchmarks:
```bash
go test ./cmd/agent -bench=. -benchmem
```
