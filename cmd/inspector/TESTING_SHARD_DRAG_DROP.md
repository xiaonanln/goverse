# Testing Shard Drag-and-Drop Feature

This document describes how to manually test the shard drag-and-drop feature in the Inspector UI.

## Prerequisites

1. etcd server running on localhost:2379
2. Goverse cluster with multiple nodes running

## Option 1: Using Docker for etcd

```bash
# Start etcd in a Docker container
docker run -d --name etcd-test \
  -p 2379:2379 \
  quay.io/coreos/etcd:latest \
  /usr/local/bin/etcd \
  --listen-client-urls http://0.0.0.0:2379 \
  --advertise-client-urls http://localhost:2379
```

## Option 2: Using Inspector Demo

The inspector demo automatically creates sample shard mappings in etcd:

```bash
# From the repository root
go run cmd/inspector/demo/main.go \
  --etcd-addr localhost:2379 \
  --nodes 3 \
  --shards 64 \
  --objects 50
```

This will:
- Start the inspector on http://localhost:8080
- Create 3 demo nodes with 64 shards
- Initialize shard mappings in etcd
- Automatically open the browser

## Testing the Drag-and-Drop Feature

1. **Navigate to Shard Management Tab**
   - Open http://localhost:8080
   - Click on the "Shard Management" tab

2. **View Current Shard Distribution**
   - You should see multiple node boxes, each showing the shards assigned to it
   - Each shard is displayed as a badge with format: `#<shard_id> (X objects)`

3. **Drag a Shard**
   - Click and hold on any shard badge
   - The badge should become semi-transparent (dragging state)
   - Drag it to a different node box

4. **Drop and Confirm**
   - Release the mouse over a different node box
   - A confirmation modal should appear showing:
     - Shard ID
     - Source node
     - Target node
   - Click "Confirm" to proceed or "Cancel" to abort

5. **Observe the Result**
   - After confirming, the shard's target node is updated in etcd
   - The shard will show as "migrating" (orange color with pulsing animation)
   - Once the target node claims the shard, it will appear under the new node

## Expected Behavior

- **Dragging State**: Shard badge becomes semi-transparent while dragging
- **Drop Zone Highlight**: Node box shows blue dashed border when hovering with a dragged shard
- **Confirmation Dialog**: Modal appears before any change is committed
- **Migration State**: Shard shows orange color and pulses during migration
- **Error Handling**: If the move fails, an alert displays the error message

## API Endpoint

The drag-and-drop feature uses the following API endpoint:

```http
POST /shards/move
Content-Type: application/json

{
  "shard_id": 5,
  "target_node": "localhost:47001"
}
```

Response on success:
```json
{
  "success": true,
  "shard_id": 5,
  "target_node": "localhost:47001",
  "message": "Shard 5 target updated to localhost:47001"
}
```

## Testing Without Inspector Demo

If you're running a real Goverse cluster:

```bash
# Start inspector with etcd connection
./inspector \
  --etcd-addr localhost:2379 \
  --etcd-prefix /goverse \
  --http-addr :8080 \
  --grpc-addr :8081
```

Then follow the same testing steps above.

## Troubleshooting

### "Consensus manager not available" error
- Ensure etcd is running and accessible
- Verify inspector was started with `--etcd-addr` flag
- Check etcd connection in inspector logs

### Shard not moving
- Verify the target node is active in the cluster
- Check that the shard migration process is working (nodes need to claim shards)
- Look at etcd to verify TargetNode was updated:
  ```bash
  etcdctl get --prefix /goverse/shard/
  ```

### Dragging doesn't work
- Ensure you're on the "Shard Management" tab
- Check browser console for JavaScript errors
- Verify the shard badges have the `draggable` class
