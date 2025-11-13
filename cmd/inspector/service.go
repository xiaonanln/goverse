package main

import (
	"encoding/json"
	"net/http"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
)

type GoverseNode = models.GoverseNode
type GoverseObject = models.GoverseObject

// CreateHTTPHandler creates the HTTP handler for the inspector web UI
func CreateHTTPHandler(pg *graph.GoverseGraph, staticDir string) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.Dir(staticDir)))
	mux.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
		nodes := pg.GetNodes()
		objects := pg.GetObjects()
		out := struct {
			GoverseNodes   []GoverseNode   `json:"goverse_nodes"`
			GoverseObjects []GoverseObject `json:"goverse_objects"`
		}{
			GoverseNodes:   nodes,
			GoverseObjects: objects,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	return mux
}
