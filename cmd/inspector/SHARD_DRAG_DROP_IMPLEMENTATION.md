# Shard Drag-and-Drop Implementation Summary

## Overview

This feature enables users to move shards between nodes in the Inspector UI by dragging and dropping shard badges. When a shard is dragged to a different node, a confirmation dialog appears, and upon confirmation, the shard's target node is updated in etcd via the ConsensusManager.

## Changes Made

### 1. Backend Changes

#### File: `cluster/consensusmanager/consensusmanager.go`
- **Added**: `StoreShardMapping` public method
  - Wrapper around the private `storeShardMapping` method
  - Allows external components (inspector) to update shard mappings
  - Takes a map of shard IDs to ShardInfo and updates them in etcd

```go
// StoreShardMapping stores shard mapping updates in etcd.
// This is an exported wrapper around storeShardMapping for use by external components (e.g., inspector).
func (cm *ConsensusManager) StoreShardMapping(ctx context.Context, updateShards map[int]ShardInfo) (int, error) {
	return cm.storeShardMapping(ctx, updateShards)
}
```

#### File: `cmd/inspector/inspectserver/inspectserver.go`
- **Added**: `handleShardMove` HTTP handler
  - Handles POST requests to `/shards/move`
  - Validates shard ID and target node
  - Updates TargetNode in etcd (CurrentNode remains unchanged)
  - Returns success/error response

**API Endpoint**: `POST /shards/move`

Request body:
```json
{
  "shard_id": 5,
  "target_node": "localhost:47001"
}
```

Response:
```json
{
  "success": true,
  "shard_id": 5,
  "target_node": "localhost:47001",
  "message": "Shard 5 target updated to localhost:47001"
}
```

### 2. Frontend Changes

#### File: `cmd/inspector/web/shardmgmt-view.js`
- **Added**: Drag-and-drop functionality
  - `setupDragAndDrop()`: Initializes drag-and-drop event listeners
  - `handleDragStart()`: Captures shard ID and source node when dragging starts
  - `handleDragEnd()`: Cleans up dragging state
  - `handleDragOver()`: Highlights drop zone while hovering
  - `handleDragLeave()`: Removes drop zone highlight
  - `handleDrop()`: Shows confirmation dialog when dropped on a different node
  - `showShardMoveConfirmation()`: Displays modal dialog with shard movement details
  - `moveShardToNode()`: Makes API call to move the shard

#### File: `cmd/inspector/web/styles.css`
- **Added**: Drag-and-drop styling
  - `.shard-badge.draggable`: Cursor changes to indicate draggability
  - `.shard-badge.dragging`: Semi-transparent while dragging
  - `.node-box.drag-over`: Highlights drop zone with blue dashed border
  - `.modal-overlay` and related classes: Confirmation modal styling

#### File: `cmd/inspector/web/index.html`
- **Added**: Confirmation modal HTML structure
  - Modal overlay with header, body, and action buttons
  - Shown when user drops a shard on a different node

### 3. Testing

#### File: `cmd/inspector/inspectserver/inspectserver_test.go`
- **Added**: Unit tests for the new endpoint
  - `TestHandleShardMove_NoConsensusManager`: Verifies error when etcd not configured
  - `TestHandleShardMove_MethodNotAllowed`: Verifies only POST is allowed
  - `TestHandleShardMove_EmptyTargetNode`: Verifies validation

All tests pass successfully.

## How It Works

1. **User Action**: User drags a shard badge from one node box to another
2. **Visual Feedback**: 
   - Dragged shard becomes semi-transparent
   - Target node box shows blue dashed border on hover
3. **Confirmation**: Modal dialog displays asking for confirmation
4. **API Call**: On confirmation, POST request sent to `/shards/move` with shard ID and target node
5. **Backend Processing**:
   - Validates shard ID and target node
   - Updates only TargetNode in etcd (via ConsensusManager.StoreShardMapping)
   - CurrentNode remains unchanged
6. **Shard Migration**: 
   - Target node detects the change via etcd watch
   - Target node claims the shard (updates CurrentNode)
   - Objects are migrated from source to target node
7. **UI Update**: 
   - Shard shows as "migrating" (orange with pulse animation) while CurrentNode != TargetNode
   - Once claimed, shard appears under target node

## Migration Process

The drag-and-drop feature only updates the **TargetNode** field in etcd. The actual migration happens through the existing shard migration process:

1. TargetNode is updated in etcd
2. Target node's ConsensusManager detects the change via watch
3. Target node calls `ClaimShardsForNode()` to claim the shard (updates CurrentNode)
4. Source node releases the shard when it has no more objects for that shard
5. Objects are migrated as part of normal operations

## Security Considerations

- No authentication/authorization implemented (inspector is internal tool)
- Validates shard ID is within valid range
- Validates target node is not empty
- Uses conditional etcd transactions based on ModRevision to prevent conflicts

## Future Enhancements

Possible improvements:
- Add "undo" functionality to revert shard movements
- Show real-time object count updates during migration
- Add progress bar for migration status
- Support multi-shard selection and batch movement
- Add confirmation on dropping to same node (currently does nothing)
