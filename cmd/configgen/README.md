# GoVerse Config Generator

A command-line tool for generating GoVerse configuration files with customizable topology.

## Usage

```bash
go run ./cmd/configgen [options] <filename.yml>
```

### Options

- `--nodes <int>`: Number of nodes to generate (default: 1)
- `--gates <int>`: Number of gates to generate (default: 1)
- `--inspector <bool>`: Include inspector configuration (default: true)

### Examples

Generate a config with 3 nodes and 2 gates with inspector:
```bash
go run ./cmd/configgen --nodes 3 --gates 2 config.yml
```

Generate a single-node config without inspector:
```bash
go run ./cmd/configgen --nodes 1 --gates 1 --inspector=false dev-config.yml
```

Generate a multi-node config without gates:
```bash
go run ./cmd/configgen --nodes 5 --gates 0 cluster-config.yml
```

## Generated Configuration

The tool generates a complete GoVerse configuration file with:

- **Cluster settings**: etcd endpoints, shard configuration
- **Node definitions**: Unique IDs, gRPC and HTTP addresses
- **Gate definitions**: Client-facing gRPC and HTTP endpoints
- **Inspector settings**: Optional monitoring and debugging UI
- **PostgreSQL configuration**: Commented template for persistence
- **Access control rules**: Commented templates for security

All generated values include:
- Comments explaining each field
- Default values shown in comments
- Production-ready port assignments
- Localhost addresses suitable for development

## Output

After generation, the tool prints:

1. **Usage instructions**: How to start nodes, gates, and inspector
2. **Service URLs**: Where to access each component
3. **Configuration tips**: Best practices and documentation references

## Integration

The generated configuration files work with:

- Node servers: `go run <your-app> --config <file> --node-id <node-id>`
- Gate servers: `go run ./cmd/gate/ --config <file> --gate-id <gate-id>`
- Inspector: `go run ./cmd/inspector/` (uses config file automatically)

See [docs/CONFIGURATION.md](../../docs/CONFIGURATION.md) for detailed configuration documentation.
