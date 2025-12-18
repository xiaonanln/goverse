package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/goverseapi"
	pb "github.com/xiaonanln/goverse/samples/handwriting/proto"
)

// HandwritingService is a distributed object that manages handwriting practice sheets
type HandwritingService struct {
	goverseapi.BaseObject
	mu     sync.Mutex
	sheets map[string]*SheetData
}

// SheetData stores information about a generated sheet
type SheetData struct {
	SheetID     string
	Text        string
	Style       string
	Repetitions int32
	SVGContent  string
	CreatedAt   int64
}

// OnCreated initializes the HandwritingService
func (h *HandwritingService) OnCreated() {
	h.sheets = make(map[string]*SheetData)
}

// GenerateSheet creates a new handwriting practice sheet
func (h *HandwritingService) GenerateSheet(ctx context.Context, req *pb.GenerateSheetRequest) (*pb.GenerateSheetResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Set defaults
	if req.Repetitions <= 0 {
		req.Repetitions = 5
	}
	if req.Style == "" {
		req.Style = "lines"
	}

	// Generate SVG content
	svg := h.generateSVG(req.Text, req.Style, req.Repetitions)

	// Store sheet data
	sheet := &SheetData{
		SheetID:     req.SheetId,
		Text:        req.Text,
		Style:       req.Style,
		Repetitions: req.Repetitions,
		SVGContent:  svg,
		CreatedAt:   time.Now().Unix(),
	}
	h.sheets[req.SheetId] = sheet

	return &pb.GenerateSheetResponse{
		SheetId:     sheet.SheetID,
		Text:        sheet.Text,
		Style:       sheet.Style,
		Repetitions: sheet.Repetitions,
		SvgContent:  sheet.SVGContent,
		CreatedAt:   sheet.CreatedAt,
	}, nil
}

// GetSheet retrieves an existing sheet
func (h *HandwritingService) GetSheet(ctx context.Context, req *pb.GetSheetRequest) (*pb.GetSheetResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	sheet, exists := h.sheets[req.SheetId]
	if !exists {
		return &pb.GetSheetResponse{
			Exists: false,
		}, nil
	}

	return &pb.GetSheetResponse{
		SheetId:     sheet.SheetID,
		Text:        sheet.Text,
		Style:       sheet.Style,
		Repetitions: sheet.Repetitions,
		SvgContent:  sheet.SVGContent,
		CreatedAt:   sheet.CreatedAt,
		Exists:      true,
	}, nil
}

// ListSheets returns all sheet IDs
func (h *HandwritingService) ListSheets(ctx context.Context, req *pb.ListSheetsRequest) (*pb.ListSheetsResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	sheetIDs := make([]string, 0, len(h.sheets))
	for id := range h.sheets {
		sheetIDs = append(sheetIDs, id)
	}

	return &pb.ListSheetsResponse{
		SheetIds: sheetIDs,
	}, nil
}

// DeleteSheet removes a sheet
func (h *HandwritingService) DeleteSheet(ctx context.Context, req *pb.DeleteSheetRequest) (*pb.DeleteSheetResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	_, exists := h.sheets[req.SheetId]
	if exists {
		delete(h.sheets, req.SheetId)
	}

	return &pb.DeleteSheetResponse{
		Success: exists,
	}, nil
}

// generateSVG creates an SVG for handwriting practice
func (h *HandwritingService) generateSVG(text string, style string, repetitions int32) string {
	const width = 800
	const height = 100
	const lineHeight = 60
	const fontSize = 40
	const padding = 20

	totalHeight := int(repetitions)*lineHeight + padding*2

	var svg strings.Builder
	svg.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, width, totalHeight, width, totalHeight))
	svg.WriteString(`<style>`)
	svg.WriteString(`.guide-text { font-family: 'Courier New', monospace; font-size: ` + fmt.Sprintf("%d", fontSize) + `px; fill: #ccc; }`)
	svg.WriteString(`.baseline { stroke: #000; stroke-width: 1; }`)
	svg.WriteString(`.midline { stroke: #ccc; stroke-width: 1; stroke-dasharray: 5,5; }`)
	svg.WriteString(`.dotted-text { font-family: 'Courier New', monospace; font-size: ` + fmt.Sprintf("%d", fontSize) + `px; fill: none; stroke: #999; stroke-width: 1; stroke-dasharray: 2,2; }`)
	svg.WriteString(`</style>`)

	// Background
	svg.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" fill="#fff"/>`, width, totalHeight))

	for i := int32(0); i < repetitions; i++ {
		y := padding + int(i)*lineHeight

		// Draw lines based on style
		if style == "lines" || style == "dotted" {
			// Baseline
			svg.WriteString(fmt.Sprintf(`<line x1="20" y1="%d" x2="%d" y2="%d" class="baseline"/>`, y+45, width-20, y+45))
			// Midline
			svg.WriteString(fmt.Sprintf(`<line x1="20" y1="%d" x2="%d" y2="%d" class="midline"/>`, y+25, width-20, y+25))
		}

		// Add text based on style
		if style == "lines" {
			// Show guide text on first line
			if i == 0 {
				svg.WriteString(fmt.Sprintf(`<text x="30" y="%d" class="guide-text">%s</text>`, y+40, escapeXML(text)))
			}
		} else if style == "dotted" {
			// Show dotted text for tracing
			svg.WriteString(fmt.Sprintf(`<text x="30" y="%d" class="dotted-text">%s</text>`, y+40, escapeXML(text)))
		}
		// For "blank", just lines, no text
	}

	svg.WriteString(`</svg>`)
	return svg.String()
}

// escapeXML escapes special XML characters
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
