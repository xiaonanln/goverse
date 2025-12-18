# Handwriting Practice Sheet Generator

A web-based application for generating handwriting practice sheets for kids, demonstrating the Goverse distributed object runtime with HTTP Gate.

## Features

- **Multiple Styles**: Generate sheets with guide text, dotted tracing, or blank lines
- **Customizable**: Choose text, number of lines, and style
- **SVG Output**: High-quality vector graphics suitable for printing
- **Distributed Architecture**: Uses Goverse virtual actor model for sheet management
- **Service-Based Design**: 5 HandwritingService objects distribute load
- **Sheet History**: View and reload previously generated sheets
- **Print & Download**: Export sheets as SVG files or print directly

## Quick Start

### Prerequisites

- Go 1.21+
- etcd (for cluster coordination)
- A web browser

### Running the Demo

1. **Start etcd**:
   ```bash
   # Using the provided script
   ./script/codespace/start-etcd.sh
   
   # Or using Docker
   docker run -d --name etcd -p 2379:2379 \
     quay.io/coreos/etcd:latest \
     /usr/local/bin/etcd \
     --listen-client-urls http://0.0.0.0:2379 \
     --advertise-client-urls http://localhost:2379
   ```

2. **Compile proto files** (first time only):
   ```bash
   cd samples/handwriting
   export PATH=$PATH:$(go env GOPATH)/bin
   ./compile-proto.sh
   ```

3. **Start the server** (runs both Node and HTTP Gate):
   ```bash
   cd samples/handwriting/server
   go run .
   # This starts:
   #   - Goverse Node on localhost:50051
   #   - HTTP Gate on localhost:49000 (gRPC) and :8080 (REST API)
   ```

4. **Open the web interface**:
   Open `samples/handwriting/web/index.html` in your browser

## Architecture

```
┌─────────────────┐     HTTP REST      ┌─────────────────┐     gRPC      ┌─────────────────────────────┐
│   Web Browser   │ ─────────────────> │   HTTP Gate     │ ────────────> │   Goverse Node              │
│   (HTML/JS)     │ <───────────────── │   (:8080)       │ <──────────── │   HandwritingService (1-5)  │
└─────────────────┘                    └─────────────────┘               │     └── SheetData (N)       │
                                                                          └─────────────────────────────┘
```

### Components

- **HandwritingService**: Distributed object (5 instances)
  - Each service can manage many sheets
  - Services are distributed across nodes for load balancing
  - Methods: GenerateSheet, GetSheet, ListSheets, DeleteSheet

- **SheetData**: Plain struct (not a distributed object)
  - Stores sheet details: text, style, repetitions, SVG content
  - Created on-demand when a user generates a sheet
  - Each sheet has a unique ID (e.g., `sheet-1702999999999`)

## Sheet Styles

1. **Lines with Guide Text**: Shows the text on the first line as a guide, with blank lines below for practice
2. **Dotted Text for Tracing**: All lines show dotted text that kids can trace over
3. **Blank Lines Only**: Just horizontal lines for freehand practice

## API Endpoints

All requests go to the HTTP Gate at `http://localhost:8080/api/v1/objects`.

### Generate Sheet
```bash
POST /api/v1/objects/call/HandwritingService/HandwritingService-1/GenerateSheet
# Request includes: sheet_id, text, style, repetitions
```

### Get Sheet
```bash
POST /api/v1/objects/call/HandwritingService/HandwritingService-1/GetSheet
# Request includes: sheet_id
```

### List Sheets
```bash
POST /api/v1/objects/call/HandwritingService/HandwritingService-1/ListSheets
```

### Delete Sheet
```bash
POST /api/v1/objects/call/HandwritingService/HandwritingService-1/DeleteSheet
# Request includes: sheet_id
```

## Usage

1. Enter text to practice (e.g., "ABC" or "Hello World")
2. Select a style (lines, dotted, or blank)
3. Choose number of lines (1-20)
4. Click "Generate Sheet"
5. Preview the sheet
6. Download as SVG or print directly

## Project Structure

```
samples/handwriting/
├── DESIGN.md              # Detailed design document
├── README.md              # This file
├── compile-proto.sh       # Proto compilation script
├── proto/
│   ├── handwriting.proto  # Protocol buffer definitions
│   └── handwriting.pb.go  # Generated Go code
├── server/
│   ├── main.go           # Server entry point
│   └── handwriting.go    # HandwritingService implementation
└── web/
    ├── index.html        # Main UI
    ├── style.css         # Styling
    └── app.js            # JavaScript application logic
```

## Development

### Adding New Styles

To add a new sheet style:

1. Update the `generateSVG` function in `server/handwriting.go`
2. Add the new style option to the dropdown in `web/index.html`
3. Rebuild and restart the server

### Multi-Node Deployment

For distributed deployment, start multiple nodes:

```bash
# Terminal 1
go run . --node-addr=localhost:50051

# Terminal 2
go run . --node-addr=localhost:50052

# Terminal 3
go run . --node-addr=localhost:50053
```

HandwritingService objects are automatically distributed across nodes via consistent hashing.

## Technical Details

- **SVG Generation**: Server-side SVG rendering with customizable fonts and line spacing
- **Object Storage**: Sheets are stored in-memory within HandwritingService objects
- **Client Storage**: Browser localStorage tracks sheet history for quick access
- **Load Distribution**: 5 service instances spread load across the cluster

## Future Enhancements

- Persistent storage (PostgreSQL) for sheets
- More font options and handwriting styles
- Word tracing with individual letter boxes
- Number and math practice sheets
- Worksheet templates (alphabet, numbers, shapes)
- User accounts and sheet collections
- PDF export option

## License

Apache-2.0 License - see the root LICENSE file.
