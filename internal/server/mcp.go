package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/graph"
	intentpkg "github.com/GrayCodeAI/yaad/internal/intent"
	"github.com/GrayCodeAI/yaad/internal/storage"
	"github.com/GrayCodeAI/yaad/internal/utils"
)

// MCPServer wraps the MCP protocol server.
type MCPServer struct {
	eng    *engine.Engine
	server *mcpserver.MCPServer
	// ToolProfile controls which tools are exposed.
	// "agent" = 8 core tools (saves ~800 tokens), "all" = all 15 tools.
	ToolProfile string
}

// NewMCPServer creates an MCP server with yaad tools registered.
// profile: "agent" (8 core tools, saves tokens) or "all" (15 tools, default).
func NewMCPServer(eng *engine.Engine, profile string) *MCPServer {
	if profile == "" {
		profile = "all"
	}
	s := &MCPServer{eng: eng, ToolProfile: profile}
	s.server = mcpserver.NewMCPServer("yaad", "0.1.0",
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithResourceCapabilities(true, false),
	)
	s.registerTools()
	return s
}

// ServeStdio starts the MCP server on stdin/stdout.
func (s *MCPServer) ServeStdio() error {
	stdio := mcpserver.NewStdioServer(s.server)
	return stdio.Listen(context.Background(), os.Stdin, os.Stdout)
}

func (s *MCPServer) registerTools() {
	add := func(tool mcp.Tool, handler mcpserver.ToolHandlerFunc) {
		s.server.AddTool(tool, handler)
	}

	// yaad_remember
	add(mcp.NewTool("yaad_remember",
		mcp.WithDescription("Store a memory node with type, metadata, and auto-linked entities"),
		mcp.WithString("content", mcp.Required(), mcp.Description("Memory content")),
		mcp.WithString("type", mcp.Description("Node type: convention|decision|bug|spec|task|preference")),
		mcp.WithString("summary", mcp.Description("Short summary")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags")),
		mcp.WithString("project", mcp.Description("Project path")),
		mcp.WithString("scope", mcp.Description("global or project")),
	), s.handleRemember)

	// yaad_recall
	add(mcp.NewTool("yaad_recall",
		mcp.WithDescription("Graph-aware search: BM25 seed → graph expansion → ranked results"),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithNumber("depth", mcp.Description("Graph expansion depth (default 2)")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
		mcp.WithString("type", mcp.Description("Filter by node type")),
		mcp.WithString("project", mcp.Description("Filter by project")),
	), s.handleRecall)

	// yaad_context
	add(mcp.NewTool("yaad_context",
		mcp.WithDescription("Get session-start context: hot-tier subgraph for injection"),
		mcp.WithString("project", mcp.Description("Project path")),
	), s.handleContext)

	// yaad_forget
	add(mcp.NewTool("yaad_forget",
		mcp.WithDescription("Archive a memory node"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Node ID to forget")),
	), s.handleForget)

	// yaad_link
	add(mcp.NewTool("yaad_link",
		mcp.WithDescription("Create an edge between two nodes"),
		mcp.WithString("from_id", mcp.Required(), mcp.Description("Source node ID")),
		mcp.WithString("to_id", mcp.Required(), mcp.Description("Target node ID")),
		mcp.WithString("type", mcp.Required(), mcp.Description("Edge type: caused_by|led_to|supersedes|relates_to|depends_on|touches|learned_in|part_of")),
	), s.handleLink)

	// yaad_unlink
	add(mcp.NewTool("yaad_unlink",
		mcp.WithDescription("Remove an edge"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Edge ID to remove")),
	), s.handleUnlink)

	// yaad_subgraph (extended — only in "all" profile)
	if s.ToolProfile == "all" {
		add(mcp.NewTool("yaad_subgraph",
			mcp.WithDescription("Get subgraph around a node via BFS"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Center node ID")),
			mcp.WithNumber("depth", mcp.Description("BFS depth (default 2)")),
		), s.handleSubgraph)

		// yaad_impact
		add(mcp.NewTool("yaad_impact",
		mcp.WithDescription("Impact analysis: what memories are affected if a file changes"),
		mcp.WithString("file", mcp.Required(), mcp.Description("File path")),
		mcp.WithNumber("depth", mcp.Description("Traversal depth (default 3)")),
	), s.handleImpact)

	// yaad_status
	add(mcp.NewTool("yaad_status",
		mcp.WithDescription("Health check: node/edge counts, session count"),
		mcp.WithString("project", mcp.Description("Project path")),
	), s.handleStatus)

	// yaad_task_update
	add(mcp.NewTool("yaad_task_update",
		mcp.WithDescription("Update a task node's content/status"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Task node ID")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Updated task content")),
	), s.handleTaskUpdate)

	// yaad_sessions
	add(mcp.NewTool("yaad_sessions",
		mcp.WithDescription("List recent sessions"),
		mcp.WithString("project", mcp.Description("Project path")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
	), s.handleSessions)

	// yaad_stale
	add(mcp.NewTool("yaad_stale",
		mcp.WithDescription("Show potentially stale memory subgraphs"),
		mcp.WithString("project", mcp.Description("Project path")),
	), s.handleStale)

	// yaad_intent (Phase 6: intent classification)
	add(mcp.NewTool("yaad_intent",
		mcp.WithDescription("Classify query intent (Why/When/Who/How/What/General) for intent-aware retrieval"),
		mcp.WithString("query", mcp.Required(), mcp.Description("Query to classify")),
	), s.handleIntent)

	// yaad_profile
	add(mcp.NewTool("yaad_profile",
		mcp.WithDescription("Get auto-maintained user/project profile: static facts + dynamic recent context"),
		mcp.WithString("project", mcp.Description("Project path")),
	), s.handleProfile)
	} // end extended tools (ToolProfile == "all")
}

// --- Tool handlers ---

func (s *MCPServer) handleRemember(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	typ := strArgOr(req, "type", "decision")
	if typ != "" && !engine.IsValidNodeType(typ) {
		return nil, fmt.Errorf("invalid node type: %q", typ)
	}
	in := engine.RememberInput{
		Content: strArg(req, "content"),
		Type:    typ,
		Summary: strArg(req, "summary"),
		Tags:    strArg(req, "tags"),
		Project: strArg(req, "project"),
		Scope:   strArgOr(req, "scope", "project"),
	}
	node, err := s.eng.Remember(ctx, in)
	if err != nil {
		return nil, err
	}
	return jsonResult(node)
}

func (s *MCPServer) handleRecall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	result, err := s.eng.Recall(ctx, engine.RecallOpts{
		Query:   strArg(req, "query"),
		Depth:   intArgOr(req, "depth", 2),
		Limit:   intArgOr(req, "limit", 10),
		Type:    strArg(req, "type"),
		Project: strArg(req, "project"),
	})
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

func (s *MCPServer) handleContext(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	result, err := s.eng.Context(ctx, strArg(req, "project"))
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

func (s *MCPServer) handleForget(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.eng.Forget(ctx, strArg(req, "id")); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText("forgotten"), nil
}

func (s *MCPServer) handleLink(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	edgeType := strArg(req, "type")
	if edgeType == "" {
		return nil, fmt.Errorf("edge type is required")
	}
	if !graph.IsValidEdgeType(edgeType) {
		return nil, fmt.Errorf("invalid edge type: %q", edgeType)
	}
	edge := &storage.Edge{
		FromID: strArg(req, "from_id"),
		ToID:   strArg(req, "to_id"),
		Type:   edgeType,
		Weight: 1.0,
	}
	edge.ID = fmt.Sprintf("%s-%s-%s", utils.ShortID(edge.FromID), utils.ShortID(edge.ToID), edge.Type)
	if err := s.eng.Graph().AddEdge(ctx, edge); err != nil {
		return nil, err
	}
	return jsonResult(edge)
}

func (s *MCPServer) handleUnlink(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := s.eng.Graph().RemoveEdge(ctx, strArg(req, "id")); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText("unlinked"), nil
}

func (s *MCPServer) handleSubgraph(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sg, err := s.eng.Graph().ExtractSubgraph(ctx, strArg(req, "id"), intArgOr(req, "depth", 2))
	if err != nil {
		return nil, err
	}
	return jsonResult(sg)
}

func (s *MCPServer) handleImpact(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ids, err := s.eng.Graph().Impact(ctx, strArg(req, "file"), intArgOr(req, "depth", 3))
	if err != nil {
		return nil, err
	}
	// Resolve IDs to nodes
	var nodes []*storage.Node
	for _, id := range ids {
		n, err := s.eng.Store().GetNode(ctx, id)
		if err == nil {
			nodes = append(nodes, n)
		}
	}
	return jsonResult(nodes)
}

func (s *MCPServer) handleStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	st, err := s.eng.Status(ctx, strArg(req, "project"))
	if err != nil {
		return nil, err
	}
	return jsonResult(st)
}

func (s *MCPServer) handleTaskUpdate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	node, err := s.eng.Store().GetNode(ctx, strArg(req, "id"))
	if err != nil {
		return nil, err
	}
	if err := s.eng.Store().SaveVersion(ctx, node.ID, node.Content, "agent", "task update"); err != nil {
		return nil, fmt.Errorf("save version: %w", err)
	}
	node.Content = strArg(req, "content")
	node.Version++
	if err := s.eng.Store().UpdateNode(ctx, node); err != nil {
		return nil, err
	}
	return jsonResult(node)
}

func (s *MCPServer) handleSessions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessions, err := s.eng.Store().ListSessions(ctx, strArg(req, "project"), intArgOr(req, "limit", 10))
	if err != nil {
		return nil, err
	}
	return jsonResult(sessions)
}

func (s *MCPServer) handleStale(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	project := strArg(req, "project")
	st, err := s.eng.Status(ctx, project)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{
		"status":      "staleness detection requires git watcher (Phase 2)",
		"nodes":       st.Nodes,
		"edges":       st.Edges,
		"sessions":    st.Sessions,
	})
}

func (s *MCPServer) handleIntent(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := strArg(req, "query")
	i := intentpkg.Classify(query)
	weights := intentpkg.Weights(i)
	return jsonResult(map[string]any{
		"query":   query,
		"intent":  i.String(),
		"weights": weights,
	})
}

func (s *MCPServer) handleProfile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p, err := s.eng.Profile(ctx, strArg(req, "project"))
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{
		"profile":   p,
		"formatted": p.Format(),
	})
}

// --- helpers ---

func strArg(req mcp.CallToolRequest, key string) string {
	if v, ok := req.GetArguments()[key].(string); ok {
		return v
	}
	return ""
}

func strArgOr(req mcp.CallToolRequest, key, def string) string {
	if v := strArg(req, key); v != "" {
		return v
	}
	return def
}

func intArgOr(req mcp.CallToolRequest, key string, def int) int {
	if v, ok := req.GetArguments()[key].(float64); ok {
		return int(v)
	}
	return def
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}
