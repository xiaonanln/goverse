# Handwriting Practice Sheet Generator - Design Document

## Overview

The Handwriting Practice Sheet Generator is a sample application demonstrating the Goverse distributed object runtime. It provides a web interface for generating customizable handwriting practice sheets for kids, showcasing how to build educational applications using virtual actors and HTTP Gate.

## Design Goals

1. **Educational Value**: Create a useful tool for parents and teachers
2. **Goverse Demonstration**: Show practical use of distributed objects and HTTP Gate
3. **Simplicity**: Keep the implementation simple and easy to understand
4. **User-Friendly**: Provide an intuitive web interface without requiring authentication
5. **Scalability**: Support multiple concurrent users with distributed service objects

## Architecture

### Distributed Objects

#### HandwritingService

The `HandwritingService` is the main distributed object that manages handwriting practice sheets.

**Design Decisions:**
- Multiple service instances (5 by default) for load distribution
- Each service can manage many sheets independently
- In-memory storage for quick access (can be extended with persistence)
- Simple mutex-based concurrency control

**Methods:**
- `GenerateSheet`: Creates a new practice sheet with specified parameters
- `GetSheet`: Retrieves an existing sheet by ID
- `ListSheets`: Returns all sheet IDs managed by this service
- `DeleteSheet`: Removes a sheet from the service

**State Management:**
- Sheets stored in a map indexed by sheet ID
- Each sheet contains: text, style, repetitions, SVG content, timestamp
- Thread-safe access using sync.Mutex

### Sheet Generation

#### SVG Rendering

Sheets are generated as SVG (Scalable Vector Graphics) for several reasons:
- Vector format scales perfectly for printing
- Text-based format easy to generate server-side
- Can be displayed directly in browsers
- Suitable for download or print

#### Styles

1. **Lines with Guide Text**
   - First line shows the text as a guide
   - Remaining lines are blank for practice
   - Includes baseline and midline guides

2. **Dotted Text for Tracing**
   - Text appears as dotted outline on all lines
   - Kids can trace over the dots
   - Helps develop muscle memory

3. **Blank Lines Only**
   - Just horizontal lines with guides
   - For freehand practice
   - No text shown

### Web Interface

#### Technology Stack
- **HTML5**: Structure
- **CSS3**: Styling with responsive design
- **Vanilla JavaScript**: No frameworks to keep it simple

#### Features
- Form for sheet customization
- Live preview of generated sheets
- Download as SVG file
- Print functionality
- Sheet history in localStorage

#### API Communication
- Uses HTTP Gate REST API
- POST requests with JSON payloads
- Base64-encoded protobuf (simplified in demo)

## Data Flow

### Sheet Generation Flow

```
User Input → JavaScript → HTTP Gate → HandwritingService → SVG Generation → Response → Display
     |                                                                            |
     └────────────────────────> LocalStorage (Sheet History) <──────────────────┘
```

1. User enters sheet parameters in web form
2. JavaScript creates GenerateSheetRequest
3. Request sent to HTTP Gate via REST API
4. Gate routes to appropriate HandwritingService instance
5. Service generates SVG content
6. Response sent back through Gate
7. JavaScript displays SVG in browser
8. Sheet ID saved to localStorage for history

### Sheet Retrieval Flow

```
Sheet History → Click → JavaScript → HTTP Gate → HandwritingService → Stored Sheet → Display
```

1. User clicks on a sheet in history
2. JavaScript sends GetSheetRequest with sheet ID
3. Gate routes to service that created the sheet
4. Service retrieves sheet from memory
5. Sheet data returned and displayed

## Scalability Considerations

### Load Distribution

- **Multiple Service Instances**: 5 HandwritingService objects spread load
- **Stateless Gate**: HTTP Gate can be scaled independently
- **Client-Side Storage**: Sheet history in browser reduces server load

### Sharding

Goverse automatically:
- Distributes service objects across nodes
- Routes requests to correct service instance
- Handles failover if a node dies

### Resource Usage

- Each sheet is ~5-10KB of SVG
- In-memory storage suitable for demo
- For production: add PostgreSQL persistence

## Implementation Details

### Proto Messages

All API calls use protocol buffers:
- `GenerateSheetRequest`: sheet_id, text, style, repetitions
- `GenerateSheetResponse`: sheet data + SVG content
- `GetSheetRequest`: sheet_id
- `GetSheetResponse`: sheet data or not found
- `ListSheetsRequest`: (empty)
- `ListSheetsResponse`: list of sheet IDs
- `DeleteSheetRequest`: sheet_id
- `DeleteSheetResponse`: success flag

### Server Implementation

**main.go:**
- Starts Goverse Node
- Starts HTTP Gate
- Creates HandwritingService objects
- Handles graceful shutdown

**handwriting.go:**
- Implements HandwritingService
- SVG generation logic
- Sheet storage and retrieval
- Mutex-protected state

### Web Implementation

**index.html:**
- Form for sheet parameters
- Preview area for generated sheets
- Sheet history section
- Action buttons (download, print, new)

**style.css:**
- Modern gradient background
- Card-based layout
- Responsive design
- Print-friendly styles

**app.js:**
- API communication
- Sheet generation and display
- LocalStorage management
- Download and print handlers

## User Experience

### First-Time User Flow

1. Open web page
2. See pre-filled example text "Hello World"
3. Click "Generate Sheet"
4. See preview appear
5. Try different styles and regenerate
6. Download or print sheet

### Returning User Flow

1. Open web page
2. See "My Sheets" section with history
3. Click on previous sheet to reload
4. Modify and regenerate
5. Create new variations

## Educational Use Cases

### For Parents
- Generate custom practice sheets for kids' names
- Create sheets for spelling words
- Practice numbers and math facts

### For Teachers
- Bulk generate sheets for whole class
- Different difficulty levels (complexity of text)
- Print worksheet packets

### For Kids
- Practice handwriting at their own pace
- Visual feedback from dotted tracing
- Build confidence with repetition

## Extension Points

### Persistence Layer
```go
// Add PostgreSQL persistence
type PersistentHandwritingService struct {
    HandwritingService
    db *sql.DB
}
```

### Additional Styles
- Cursive/script fonts
- Letter boxes (one per character)
- Lined paper (wide, college, narrow)
- Graph paper for math

### Advanced Features
- User accounts
- Sheet collections/workbooks
- Progress tracking
- Sharing sheets with others

### Font Options
- Different print styles
- Custom fonts upload
- Adjustable size and spacing

## Testing Strategy

### Unit Tests
- Sheet generation logic
- SVG rendering correctness
- Data storage and retrieval

### Integration Tests
- End-to-end sheet creation
- API endpoint functionality
- Multi-node deployment

### Manual Testing
- Browser compatibility
- Print output quality
- Mobile responsiveness
- Download functionality

## Deployment

### Development
```bash
./script/codespace/start-etcd.sh
cd samples/handwriting/server
go run .
# Open web/index.html in browser
```

### Production
1. Deploy etcd cluster
2. Deploy multiple Goverse nodes
3. Deploy HTTP Gates with load balancer
4. Serve web files from CDN or static hosting
5. Configure proper CORS and security headers

## Security Considerations

### Current Implementation (Demo)
- No authentication or authorization
- Public access to all sheets
- No rate limiting
- No input validation beyond client-side

### Production Recommendations
- Add user authentication (OAuth, JWT)
- Implement object-level access control
- Rate limiting on Gate
- Input sanitization and validation
- HTTPS only
- Content Security Policy headers

## Performance Characteristics

### Sheet Generation
- SVG generation: <1ms
- Network round-trip: ~10-50ms
- Browser rendering: <100ms
- Total user experience: <200ms

### Storage
- Per sheet: ~5-10KB
- 1000 sheets: ~5-10MB memory
- Lightweight for in-memory demo

### Scalability
- Single node: 1000s of sheets/minute
- Multi-node: linear scaling with nodes
- Bottleneck: typically browser rendering

## Comparison to Similar Systems

### Traditional Web App
- Would use database-backed REST API
- Single point of failure
- Manual load balancing

### Goverse Approach
- Virtual actors handle state
- Automatic distribution
- Built-in fault tolerance
- Cleaner separation of concerns

## Lessons Learned

### What Worked Well
- SVG generation is simple and effective
- HTTP Gate REST API easy to use from browser
- Multiple service instances distribute load naturally
- LocalStorage for history improves UX

### What Could Be Improved
- Real persistence layer needed for production
- Better error handling and recovery
- More sophisticated SVG layouts
- Client-side SVG preview before server call

## Future Directions

1. **Enhanced Typography**: Support for more fonts and styles
2. **Interactive Editing**: Client-side SVG editing before finalization
3. **Templates**: Pre-made worksheet templates
4. **Gamification**: Progress tracking and achievements
5. **Collaboration**: Share worksheets with others
6. **Analytics**: Track which sheets are most popular
7. **Export Options**: PDF, PNG, printable formats
8. **Mobile App**: Native iOS/Android versions

## Conclusion

The Handwriting Practice Sheet Generator demonstrates how Goverse can be used to build practical applications with distributed objects. The design balances simplicity for demonstration purposes with enough features to be genuinely useful. It showcases key Goverse concepts: virtual actors, HTTP Gate, distributed state, and automatic scaling.
